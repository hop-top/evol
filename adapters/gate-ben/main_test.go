package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBen writes an executable shell script that plays ben: `run` cats
// the run fixture, `show <id>` cats the show fixture. Returns the
// script path.
func fakeBen(t *testing.T, runJSON, showJSON string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ben script requires a POSIX shell")
	}
	dir := t.TempDir()
	runPath := filepath.Join(dir, "run.json")
	showPath := filepath.Join(dir, "show.json")
	if err := os.WriteFile(runPath, []byte(runJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(showPath, []byte(showJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\nrun) cat %q ;;\nshow) cat %q ;;\n*) echo \"unexpected verb $1\" >&2; exit 64 ;;\nesac\n", runPath, showPath)
	bin := filepath.Join(dir, "ben")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { // #nosec G306 -- test fixture must be executable
		t.Fatal(err)
	}
	return bin
}

func gateRequest(t *testing.T, baseline string, metrics ...metric) string {
	t.Helper()
	req := map[string]any{
		"evol": "1", "port": "gate", "action": "check",
		"candidate_ref": "cand-a",
		"suite":         "core",
		"metrics":       metrics,
	}
	if baseline != "" {
		req["baseline"] = map[string]string{"run_id": baseline}
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runAdapter(t *testing.T, benBin, input string) (int, response, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	getenv := func(k string) string {
		if k == "EVOL_BEN_BIN" {
			return benBin
		}
		return ""
	}
	code := run(strings.NewReader(input), &stdout, &stderr, getenv)
	var resp response
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			t.Fatalf("response not JSON: %v\nstdout: %s", err, stdout.String())
		}
	}
	return code, resp, stderr.String()
}

const candRunJSON = `{"run_id":"run-cand","suite":"core","candidates":[
  {"name":"cand-a","metrics":{"latency_ms":100,"accuracy":0.90}}]}`

func TestPassWithinThresholds(t *testing.T) {
	base := `{"run_id":"run-base","suite":"core","candidates":[
	  {"name":"cand-a","metrics":{"latency_ms":102,"accuracy":0.89}}]}`
	bin := fakeBen(t, candRunJSON, base)
	code, resp, stderr := runAdapter(t, bin, gateRequest(t, "run-base",
		metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5},
		metric{Name: "accuracy", Direction: "max", ThresholdDelta: 0.05},
	))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !resp.Pass {
		t.Fatalf("want pass, got fail: %s", resp.Reason)
	}
	if resp.RunID != "run-cand" {
		t.Fatalf("run_id = %q, want run-cand", resp.RunID)
	}
	if len(resp.Deltas) != 2 {
		t.Fatalf("deltas = %d, want 2", len(resp.Deltas))
	}
}

func TestRegressionFailsBothDirections(t *testing.T) {
	// candidate: latency 100 (vs base 80 → +20 > 5 regression for min),
	// accuracy 0.90 (vs base 0.99 → -0.09 < -0.05 regression for max).
	base := `{"run_id":"run-base","suite":"core","candidates":[
	  {"name":"cand-a","metrics":{"latency_ms":80,"accuracy":0.99}}]}`
	bin := fakeBen(t, candRunJSON, base)
	code, resp, stderr := runAdapter(t, bin, gateRequest(t, "run-base",
		metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5},
		metric{Name: "accuracy", Direction: "max", ThresholdDelta: 0.05},
	))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if resp.Pass {
		t.Fatal("want fail, got pass")
	}
	for _, want := range []string{"latency_ms regressed", "accuracy regressed"} {
		if !strings.Contains(resp.Reason, want) {
			t.Errorf("reason missing %q: %s", want, resp.Reason)
		}
	}
}

func TestMissingMetricFails(t *testing.T) {
	base := `{"run_id":"run-base","suite":"core","candidates":[
	  {"name":"cand-a","metrics":{"latency_ms":100}}]}`
	bin := fakeBen(t, candRunJSON, base)
	code, resp, _ := runAdapter(t, bin, gateRequest(t, "run-base",
		metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5},
		metric{Name: "cost_usd", Direction: "min", ThresholdDelta: 0.01},
	))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if resp.Pass {
		t.Fatal("want fail on missing metric, got pass")
	}
	if !strings.Contains(resp.Reason, `metric "cost_usd" missing`) {
		t.Fatalf("reason = %s", resp.Reason)
	}
}

func TestNoBaselineFirstRunPasses(t *testing.T) {
	bin := fakeBen(t, candRunJSON, `{}`)
	code, resp, stderr := runAdapter(t, bin, gateRequest(t, "",
		metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5},
	))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !resp.Pass {
		t.Fatalf("want pass, got fail: %s", resp.Reason)
	}
	if resp.Reason != "no baseline (first run)" {
		t.Fatalf("reason = %q", resp.Reason)
	}
	if resp.RunID != "run-cand" {
		t.Fatalf("run_id = %q, want run-cand for engine pinning", resp.RunID)
	}
}

func TestMetaEnvelopeUnwrapped(t *testing.T) {
	base := `{"_meta":{"source":"db","method":"show"},
	  "run_id":"run-base","suite":"core","candidates":[
	  {"name":"cand-a","metrics":{"latency_ms":101}}]}`
	bin := fakeBen(t, candRunJSON, base)
	code, resp, stderr := runAdapter(t, bin, gateRequest(t, "run-base",
		metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5},
	))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !resp.Pass {
		t.Fatalf("want pass, got fail: %s", resp.Reason)
	}
}

func TestDataEnvelopeUnwrapped(t *testing.T) {
	base := `{"_meta":{"source":"db"},"data":{"run_id":"run-base","suite":"core",
	  "candidates":[{"name":"cand-a","metrics":{"latency_ms":101}}]}}`
	bin := fakeBen(t, candRunJSON, base)
	code, resp, stderr := runAdapter(t, bin, gateRequest(t, "run-base",
		metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5},
	))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !resp.Pass {
		t.Fatalf("want pass, got fail: %s", resp.Reason)
	}
}

func TestBenMissingIsAdapterError(t *testing.T) {
	code, _, stderr := runAdapter(t, filepath.Join(t.TempDir(), "no-such-ben"),
		gateRequest(t, "run-base", metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5}))
	if code == 0 {
		t.Fatal("want non-zero exit when ben is missing: a gate that cannot run must not pass")
	}
	if !strings.Contains(stderr, "candidate run") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestBadRequestsAreAdapterErrors(t *testing.T) {
	bin := fakeBen(t, candRunJSON, `{}`)
	cases := map[string]string{
		"not json":        `{"evol":`,
		"wrong version":   `{"evol":"2","port":"gate","action":"check","suite":"s","metrics":[{"name":"m","direction":"min","threshold_delta":1}]}`,
		"wrong port":      `{"evol":"1","port":"scorer","action":"check","suite":"s","metrics":[{"name":"m","direction":"min","threshold_delta":1}]}`,
		"no suite":        `{"evol":"1","port":"gate","action":"check","metrics":[{"name":"m","direction":"min","threshold_delta":1}]}`,
		"no metrics":      `{"evol":"1","port":"gate","action":"check","suite":"s","metrics":[]}`,
		"bad direction":   `{"evol":"1","port":"gate","action":"check","suite":"s","metrics":[{"name":"m","direction":"up","threshold_delta":1}]}`,
		"negative delta":  `{"evol":"1","port":"gate","action":"check","suite":"s","metrics":[{"name":"m","direction":"min","threshold_delta":-1}]}`,
		"ambiguous match": `{"evol":"1","port":"gate","action":"check","candidate_ref":"nope","suite":"s","metrics":[{"name":"m","direction":"min","threshold_delta":1}]}`,
	}
	multi := `{"run_id":"run-cand","suite":"core","candidates":[
	  {"name":"a","metrics":{}},{"name":"b","metrics":{}}]}`
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			b := bin
			if name == "ambiguous match" {
				b = fakeBen(t, multi, `{}`)
			}
			code, _, _ := runAdapter(t, b, input)
			if code == 0 {
				t.Fatal("want non-zero exit")
			}
		})
	}
}

func TestSoleCandidateFallback(t *testing.T) {
	// candidate_ref not matching by name, but run has exactly one row.
	soleRun := `{"run_id":"run-cand","suite":"core","candidates":[
	  {"name":"claude-sonnet","metrics":{"latency_ms":100}}]}`
	bin := fakeBen(t, soleRun, `{}`)
	code, resp, stderr := runAdapter(t, bin, gateRequest(t, "",
		metric{Name: "latency_ms", Direction: "min", ThresholdDelta: 5},
	))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !resp.Pass {
		t.Fatalf("want pass: %s", resp.Reason)
	}
}
