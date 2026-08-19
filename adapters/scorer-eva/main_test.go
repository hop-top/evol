package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScript drops an executable shell script into dir and returns its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	return path
}

func writeContract(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "contract.yaml")
	if err := os.WriteFile(path, []byte("name: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func request(t *testing.T) string {
	t.Helper()
	return `{"evol":"1","port":"scorer","action":"score",` +
		`"case":{"id":"c1","input":"say hi"},` +
		`"transcript":{"output":"hi there"}}`
}

func runAdapter(t *testing.T, input string) (scoreResponse, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(strings.NewReader(input), &out)
	var resp scoreResponse
	if err == nil {
		if uerr := json.Unmarshal(out.Bytes(), &resp); uerr != nil {
			t.Fatalf("response not JSON: %v\n%s", uerr, out.String())
		}
	}
	return resp, err
}

func TestScorePassMeanAndReasons(t *testing.T) {
	dir := t.TempDir()
	report := `{"contract":"c","passed":true,"duration_ms":5,` +
		`"evaluators":[` +
		`{"name":"contains","mode":"binary","min_score":1,"score":0.8,"passed":true,"reason":"found phrase"},` +
		`{"name":"word_count","mode":"threshold","min_score":0.5,"score":{"value":0.4},"passed":false,"reason":"too short"}` +
		`],"skipped":[]}`
	eva := writeScript(t, dir, "eva", "cat >/dev/null\nprintf '%s' '"+report+"'\nexit 0\n")
	t.Setenv("EVOL_EVA_BIN", eva)
	t.Setenv("EVOL_EVA_CONTRACT", writeContract(t, dir))

	resp, err := runAdapter(t, request(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resp.Score.Value, 0.6; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("value = %v, want %v", got, want)
	}
	// Failing evaluator's reason must come first.
	if !strings.HasPrefix(resp.Score.Reason, "word_count: too short") {
		t.Fatalf("reason not failing-first: %q", resp.Score.Reason)
	}
	if !strings.Contains(resp.Score.Reason, "contains: found phrase") {
		t.Fatalf("reason missing passing entry: %q", resp.Score.Reason)
	}
}

func TestScoreFailReportOnStderr(t *testing.T) {
	dir := t.TempDir()
	report := `{"contract":"c","passed":false,"duration_ms":5,` +
		`"evaluators":[{"name":"regex","mode":"binary","min_score":1,"score":0.0,"passed":false,"reason":"no match"}],` +
		`"skipped":[]}`
	eva := writeScript(t, dir, "eva", "cat >/dev/null\nprintf '%s' '"+report+"' >&2\nexit 1\n")
	t.Setenv("EVOL_EVA_BIN", eva)
	t.Setenv("EVOL_EVA_CONTRACT", writeContract(t, dir))

	resp, err := runAdapter(t, request(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Score.Value != 0.0 {
		t.Fatalf("value = %v, want 0.0", resp.Score.Value)
	}
	if !strings.Contains(resp.Score.Reason, "regex: no match") {
		t.Fatalf("reason = %q", resp.Score.Reason)
	}
}

func TestScoreFallbackToPassedWhenNoScores(t *testing.T) {
	dir := t.TempDir()
	report := `{"contract":"c","passed":true,"duration_ms":5,"evaluators":[],"skipped":["llm_judge"]}`
	eva := writeScript(t, dir, "eva", "cat >/dev/null\nprintf '%s' '"+report+"'\nexit 0\n")
	t.Setenv("EVOL_EVA_BIN", eva)
	t.Setenv("EVOL_EVA_CONTRACT", writeContract(t, dir))

	resp, err := runAdapter(t, request(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Score.Value != 1.0 {
		t.Fatalf("value = %v, want 1.0", resp.Score.Value)
	}
	if !strings.Contains(resp.Score.Reason, "skipped: llm_judge") {
		t.Fatalf("reason = %q", resp.Score.Reason)
	}
}

func TestScoreExitTwoIsAdapterError(t *testing.T) {
	dir := t.TempDir()
	eva := writeScript(t, dir, "eva", "cat >/dev/null\necho 'contract invalid' >&2\nexit 2\n")
	t.Setenv("EVOL_EVA_BIN", eva)
	t.Setenv("EVOL_EVA_CONTRACT", writeContract(t, dir))

	if _, err := runAdapter(t, request(t)); err == nil {
		t.Fatal("want error on eva exit 2")
	}
}

func TestScoreTimeout(t *testing.T) {
	dir := t.TempDir()
	eva := writeScript(t, dir, "eva", "cat >/dev/null\nsleep 5\n")
	t.Setenv("EVOL_EVA_BIN", eva)
	t.Setenv("EVOL_EVA_CONTRACT", writeContract(t, dir))
	t.Setenv("EVOL_EVA_TIMEOUT", "100ms")

	if _, err := runAdapter(t, request(t)); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

func TestScoreMissingBinaryIsAdapterError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_EVA_BIN", filepath.Join(dir, "definitely-not-here"))
	t.Setenv("EVOL_EVA_CONTRACT", writeContract(t, dir))

	if _, err := runAdapter(t, request(t)); err == nil {
		t.Fatal("want error on missing binary")
	}
}

func TestScoreMissingContractIsAdapterError(t *testing.T) {
	dir := t.TempDir()
	eva := writeScript(t, dir, "eva", "exit 0\n")
	t.Setenv("EVOL_EVA_BIN", eva)
	t.Setenv("EVOL_EVA_CONTRACT", filepath.Join(dir, "missing.yaml"))

	if _, err := runAdapter(t, request(t)); err == nil {
		t.Fatal("want error on missing contract")
	}
}

func TestScoreRejectsWrongEnvelope(t *testing.T) {
	dir := t.TempDir()
	eva := writeScript(t, dir, "eva", "exit 0\n")
	t.Setenv("EVOL_EVA_BIN", eva)
	t.Setenv("EVOL_EVA_CONTRACT", writeContract(t, dir))

	bad := `{"evol":"1","port":"corpus","action":"score","case":{},"transcript":{}}`
	if _, err := runAdapter(t, bad); err == nil {
		t.Fatal("want error on wrong port")
	}
}
