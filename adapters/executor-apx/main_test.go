package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runAdapter invokes realMain in-process with a request and an env map.
func runAdapter(t *testing.T, req map[string]any, envs map[string]string) (int, response, string) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout, stderr bytes.Buffer
	getenv := func(k string) string { return envs[k] }
	code := realMain(bytes.NewReader(raw), &stdout, &stderr, getenv)

	var resp response
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			t.Fatalf("parse response: %v\nstdout: %s", err, stdout.String())
		}
	}
	return code, resp, stderr.String()
}

func baseRequest() map[string]any {
	return map[string]any{
		"evol":          "1",
		"port":          "executor",
		"action":        "run",
		"candidate_ref": "staging/cand-01",
		"case":          map[string]any{"id": "case-1", "input": "hello world"},
		"env":           map[string]any{},
	}
}

func script(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write script: %v", err)
	}
	return path
}

func argvJSON(t *testing.T, argv ...string) string {
	t.Helper()
	raw, err := json.Marshal(argv)
	if err != nil {
		t.Fatalf("marshal argv: %v", err)
	}
	return string(raw)
}

func TestHappyPath(t *testing.T) {
	child := script(t, "child.sh", `printf 'out:%s' "$1"`)
	code, resp, stderr := runAdapter(t, baseRequest(), map[string]string{
		"EVOL_EXEC_CMD": argvJSON(t, child, "{input}"),
	})
	if code != 0 {
		t.Fatalf("adapter exit %d, stderr: %s", code, stderr)
	}
	if resp.Evol != "1" || resp.Port != "executor" || resp.Action != "run" {
		t.Errorf("envelope echo wrong: %+v", resp)
	}
	if resp.Transcript == nil {
		t.Fatal("transcript missing")
	}
	if resp.Transcript.Output != "out:hello world" {
		t.Errorf("output = %q", resp.Transcript.Output)
	}
	if resp.Transcript.ExitCode == nil || *resp.Transcript.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", resp.Transcript.ExitCode)
	}
	if resp.Transcript.ToolCalls == nil || len(resp.Transcript.ToolCalls) != 0 {
		t.Errorf("tool_calls = %v, want empty array", resp.Transcript.ToolCalls)
	}
	if resp.Error != "" {
		t.Errorf("unexpected run error %q", resp.Error)
	}
}

func TestCandidateRefSubstitution(t *testing.T) {
	child := script(t, "child.sh", `printf '%s' "$1"`)
	req := baseRequest()
	code, resp, stderr := runAdapter(t, req, map[string]string{
		"EVOL_EXEC_CMD": argvJSON(t, child, "{candidate_ref}"),
	})
	if code != 0 {
		t.Fatalf("adapter exit %d, stderr: %s", code, stderr)
	}
	if resp.Transcript.Output != "staging/cand-01" {
		t.Errorf("output = %q", resp.Transcript.Output)
	}
}

func TestXRREnvInjection(t *testing.T) {
	child := script(t, "child.sh", `printf 'MODE=%s DIR=%s' "$XRR_MODE" "$XRR_CASSETTE_DIR"`)
	root := t.TempDir()
	req := baseRequest()
	req["env"] = map[string]any{"mode": "replay"} // request mode wins over layer default
	code, resp, stderr := runAdapter(t, req, map[string]string{
		"EVOL_EXEC_CMD":          argvJSON(t, child),
		"EVOL_XRR_MODE":          "record",
		"EVOL_XRR_CASSETTE_ROOT": root,
	})
	if code != 0 {
		t.Fatalf("adapter exit %d, stderr: %s", code, stderr)
	}
	out := resp.Transcript.Output
	if !strings.Contains(out, "MODE=replay") {
		t.Errorf("request mode should win: %q", out)
	}
	if !strings.Contains(out, "DIR="+root) {
		t.Errorf("cassette dir not under root: %q", out)
	}
	// Per-candidate subdir must exist.
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Errorf("expected one cassette subdir under root, got %v (err %v)", entries, err)
	}
}

func TestFrozenModeWithoutLayerIsAdapterFailure(t *testing.T) {
	child := script(t, "child.sh", `true`)
	req := baseRequest()
	req["env"] = map[string]any{"mode": "replay"}
	code, _, stderr := runAdapter(t, req, map[string]string{
		"EVOL_EXEC_CMD": argvJSON(t, child),
	})
	if code == 0 {
		t.Fatal("expected adapter failure for replay without xrr layer")
	}
	if !strings.Contains(stderr, "EVOL_XRR_MODE") {
		t.Errorf("stderr should point at the missing layer: %s", stderr)
	}
}

func TestAPSWrappingAndEnvelopeStripping(t *testing.T) {
	// Fake aps: records its argv, emits envelope lines around real output.
	argvLog := filepath.Join(t.TempDir(), "aps-argv")
	aps := script(t, "aps", fmt.Sprintf(`printf '%%s\n' "$@" > %q
printf '[exec] sh -c child\n'
printf '{"event":"exec","cmd":"child"}\n'
printf 'real child line 1\n'
printf '{"not":"envelope"}\n'
printf 'real child line 2\n'
printf '{"event":"exit","code":0}\n'
printf '[exit] 0\n'`, argvLog))
	child := script(t, "child.sh", `true`)
	root := t.TempDir()
	req := baseRequest()
	req["env"] = map[string]any{"mode": "record"}
	code, resp, stderr := runAdapter(t, req, map[string]string{
		"EVOL_EXEC_CMD":          argvJSON(t, child, "{input}"),
		"EVOL_APS_PROFILE":       "cand0",
		"EVOL_APS_BIN":           aps,
		"EVOL_XRR_MODE":          "record",
		"EVOL_XRR_CASSETTE_ROOT": root,
	})
	if code != 0 {
		t.Fatalf("adapter exit %d, stderr: %s", code, stderr)
	}
	out := resp.Transcript.Output
	for _, banned := range []string{"[exec]", "[exit]", `"event":"exec"`, `"event":"exit"`} {
		if strings.Contains(out, banned) {
			t.Errorf("envelope line leaked into output: %q", out)
		}
	}
	for _, want := range []string{"real child line 1", "real child line 2", `{"not":"envelope"}`} {
		if !strings.Contains(out, want) {
			t.Errorf("real output missing %q: %q", want, out)
		}
	}

	// Flag-order contract: run <profile> --env ... --env ... -- <child...>
	rawArgv, err := os.ReadFile(argvLog) //nolint:gosec // test-owned path
	if err != nil {
		t.Fatalf("fake aps did not record argv: %v", err)
	}
	got := strings.Fields(string(rawArgv))
	sep := -1
	for i, a := range got {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep == -1 {
		t.Fatalf("no -- separator in aps argv: %v", got)
	}
	pre := strings.Join(got[:sep], " ")
	if !strings.HasPrefix(pre, "run cand0") {
		t.Errorf("aps argv should start with run <profile>: %v", got)
	}
	if !strings.Contains(pre, "--env XRR_MODE=record") || !strings.Contains(pre, "--env XRR_CASSETTE_DIR=") {
		t.Errorf("--env flags must precede --: %v", got)
	}
	post := strings.Join(got[sep+1:], " ")
	if strings.Contains(post, "--env") {
		t.Errorf("--env leaked after --: %v", got)
	}
}

func TestTimeoutIsRunFailure(t *testing.T) {
	child := script(t, "slow.sh", `sleep 5`)
	start := time.Now()
	code, resp, stderr := runAdapter(t, baseRequest(), map[string]string{
		"EVOL_EXEC_CMD":     argvJSON(t, child),
		"EVOL_EXEC_TIMEOUT": "300ms",
	})
	if code != 0 {
		t.Fatalf("timeout must be a run failure (exit 0), got %d: %s", code, stderr)
	}
	if !strings.Contains(resp.Error, "timeout") {
		t.Errorf("error = %q, want timeout", resp.Error)
	}
	if resp.Transcript == nil {
		t.Error("partial transcript expected on timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("child not killed on deadline (took %s)", elapsed)
	}
}

func TestNonZeroChildExitIsData(t *testing.T) {
	child := script(t, "fail.sh", `printf 'partial'; exit 7`)
	code, resp, stderr := runAdapter(t, baseRequest(), map[string]string{
		"EVOL_EXEC_CMD": argvJSON(t, child),
	})
	if code != 0 {
		t.Fatalf("non-zero child exit must not fail the adapter, got %d: %s", code, stderr)
	}
	if resp.Transcript.ExitCode == nil || *resp.Transcript.ExitCode != 7 {
		t.Errorf("exit_code = %v, want 7", resp.Transcript.ExitCode)
	}
	if resp.Error != "" {
		t.Errorf("non-zero exit is data, not a run error: %q", resp.Error)
	}
	if resp.Transcript.Output != "partial" {
		t.Errorf("output = %q", resp.Transcript.Output)
	}
}

func TestAdapterFailures(t *testing.T) {
	child := script(t, "child.sh", `true`)
	cases := []struct {
		name  string
		stdin string
		envs  map[string]string
	}{
		{"bad json", "{nope", map[string]string{"EVOL_EXEC_CMD": argvJSON(t, child)}},
		{"wrong port", `{"evol":"1","port":"corpus","action":"run"}`, map[string]string{"EVOL_EXEC_CMD": argvJSON(t, child)}},
		{"wrong version", `{"evol":"2","port":"executor","action":"run"}`, map[string]string{"EVOL_EXEC_CMD": argvJSON(t, child)}},
		{"missing cmd", `{"evol":"1","port":"executor","action":"run"}`, map[string]string{}},
		{"cmd not array", `{"evol":"1","port":"executor","action":"run"}`, map[string]string{"EVOL_EXEC_CMD": `"not-an-array"`}},
		{"bad timeout", `{"evol":"1","port":"executor","action":"run"}`, map[string]string{"EVOL_EXEC_CMD": argvJSON(t, child), "EVOL_EXEC_TIMEOUT": "soon"}},
		{"missing binary", `{"evol":"1","port":"executor","action":"run"}`, map[string]string{"EVOL_EXEC_CMD": `["/nonexistent/definitely-not-here"]`}},
		{"xrr mode without dirs", `{"evol":"1","port":"executor","action":"run"}`, map[string]string{"EVOL_EXEC_CMD": argvJSON(t, child), "EVOL_XRR_MODE": "replay"}},
		{"bad mode", `{"evol":"1","port":"executor","action":"run","env":{"mode":"freeze"}}`, map[string]string{"EVOL_EXEC_CMD": argvJSON(t, child)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			getenv := func(k string) string { return tc.envs[k] }
			code := realMain(strings.NewReader(tc.stdin), &stdout, &stderr, getenv)
			if code == 0 {
				t.Errorf("expected adapter failure, got exit 0 (stdout %q)", stdout.String())
			}
			if stderr.Len() == 0 {
				t.Error("adapter failure must write diagnostics to stderr")
			}
		})
	}
}

func TestLiveModeSkipsInjectionEvenWithLayer(t *testing.T) {
	child := script(t, "child.sh", `printf 'MODE=%s' "$XRR_MODE"`)
	root := t.TempDir()
	req := baseRequest()
	req["env"] = map[string]any{"mode": "live"}
	code, resp, stderr := runAdapter(t, req, map[string]string{
		"EVOL_EXEC_CMD":          argvJSON(t, child),
		"EVOL_XRR_MODE":          "replay",
		"EVOL_XRR_CASSETTE_ROOT": root,
	})
	if code != 0 {
		t.Fatalf("adapter exit %d, stderr: %s", code, stderr)
	}
	if resp.Transcript.Output != "MODE=" {
		t.Errorf("live mode must not inject XRR env: %q", resp.Transcript.Output)
	}
}
