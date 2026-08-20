package main

import (
	"bytes"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "runner-xrr-bin")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tempdir: %v\n", err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "runner-xrr")
	cmd := osexec.Command("go", "build", "-buildvcs=false", "-o", binPath, ".") //nolint:gosec // G204: test builds its own binary
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// writeFakeRunner writes a shell runner that appends to $SIDE_EFFECT_FILE
// (proof of a real spawn), then prints an output derived from the
// candidate body and stdin, exiting with $FAKE_EXIT (default 0).
func writeFakeRunner(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "fake-runner.sh")
	script := `#!/bin/sh
echo spawned >> "$SIDE_EFFECT_FILE"
printf 'cand=%s input=%s' "$(cat "$EVOL_CANDIDATE_REF")" "$(cat)"
exit "${FAKE_EXIT:-0}"
`
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil { // #nosec G306 -- executable test script
		t.Fatal(err)
	}
	return p
}

func writeCandidate(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type shimResult struct {
	stdout, stderr string
	exit           int
}

func runShim(t *testing.T, env map[string]string, stdin string, args ...string) shimResult {
	t.Helper()
	cmd := osexec.Command(binPath, args...) //nolint:gosec // G204: test executes its own built binary
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	// Scrub ambient xrr/evol vars, then apply the case's env.
	base := os.Environ()
	scrubbed := base[:0:0]
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "XRR_MODE", "XRR_CASSETTE_DIR", "EVOL_CANDIDATE_REF", "EVOL_PROVIDER", "SIDE_EFFECT_FILE", "FAKE_EXIT":
		default:
			scrubbed = append(scrubbed, kv)
		}
	}
	for k, v := range env {
		scrubbed = append(scrubbed, k+"="+v)
	}
	cmd.Env = scrubbed
	err := cmd.Run()
	res := shimResult{stdout: out.String(), stderr: errb.String()}
	if err == nil {
		res.exit = 0
	} else if ee, ok := err.(*osexec.ExitError); ok {
		res.exit = ee.ExitCode()
	} else {
		t.Fatalf("run shim: %v", err)
	}
	return res
}

func cassetteFiles(t *testing.T, dir, suffix string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "exec-*"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestRecordReplayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	runner := writeFakeRunner(t, dir)
	cand := writeCandidate(t, dir, "cand.md", "skill-v1")
	cassettes := filepath.Join(dir, "cassettes")
	side := filepath.Join(dir, "side-effect")

	env := map[string]string{
		"XRR_MODE":           "record",
		"XRR_CASSETTE_DIR":   cassettes,
		"EVOL_CANDIDATE_REF": cand,
		"EVOL_PROVIDER":      "claude://haiku",
		"SIDE_EFFECT_FILE":   side,
	}
	rec := runShim(t, env, "case-input-1", runner)
	if rec.exit != 0 {
		t.Fatalf("record exit=%d stderr=%s", rec.exit, rec.stderr)
	}
	want := "cand=skill-v1 input=case-input-1"
	if rec.stdout != want {
		t.Fatalf("record stdout=%q want %q", rec.stdout, want)
	}
	if _, err := os.Stat(side); err != nil {
		t.Fatal("record mode must spawn the real runner")
	}
	if n := len(cassetteFiles(t, cassettes, ".req.yaml")); n != 1 {
		t.Fatalf("want 1 req cassette, got %d", n)
	}

	// Replay: remove the spawn proof; output must come from the cassette.
	if err := os.Remove(side); err != nil {
		t.Fatal(err)
	}
	env["XRR_MODE"] = "replay"
	rep := runShim(t, env, "case-input-1", runner)
	if rep.exit != 0 {
		t.Fatalf("replay exit=%d stderr=%s", rep.exit, rep.stderr)
	}
	if rep.stdout != want {
		t.Fatalf("replay stdout=%q want %q", rep.stdout, want)
	}
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatal("replay mode must NOT spawn the real runner")
	}
}

func TestDistinctCandidatesDistinctFingerprints(t *testing.T) {
	dir := t.TempDir()
	runner := writeFakeRunner(t, dir)
	candA := writeCandidate(t, dir, "a.md", "skill-A")
	candB := writeCandidate(t, dir, "b.md", "skill-B")
	cassettes := filepath.Join(dir, "cassettes")
	side := filepath.Join(dir, "side-effect")

	env := map[string]string{
		"XRR_MODE":         "record",
		"XRR_CASSETTE_DIR": cassettes,
		"SIDE_EFFECT_FILE": side,
	}
	env["EVOL_CANDIDATE_REF"] = candA
	if r := runShim(t, env, "same-input", runner); r.exit != 0 {
		t.Fatalf("record A: %d %s", r.exit, r.stderr)
	}
	env["EVOL_CANDIDATE_REF"] = candB
	if r := runShim(t, env, "same-input", runner); r.exit != 0 {
		t.Fatalf("record B: %d %s", r.exit, r.stderr)
	}
	if n := len(cassetteFiles(t, cassettes, ".req.yaml")); n != 2 {
		t.Fatalf("distinct candidates must not collide: want 2 cassettes, got %d", n)
	}

	env["XRR_MODE"] = "replay"
	env["EVOL_CANDIDATE_REF"] = candA
	if r := runShim(t, env, "same-input", runner); r.stdout != "cand=skill-A input=same-input" {
		t.Fatalf("replay A stdout=%q", r.stdout)
	}
	env["EVOL_CANDIDATE_REF"] = candB
	if r := runShim(t, env, "same-input", runner); r.stdout != "cand=skill-B input=same-input" {
		t.Fatalf("replay B stdout=%q", r.stdout)
	}
}

func TestReplayMissDistinctExit(t *testing.T) {
	dir := t.TempDir()
	runner := writeFakeRunner(t, dir)
	cand := writeCandidate(t, dir, "cand.md", "skill-v1")
	cassettes := filepath.Join(dir, "cassettes")
	if err := os.MkdirAll(cassettes, 0o750); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"XRR_MODE":           "replay",
		"XRR_CASSETTE_DIR":   cassettes,
		"EVOL_CANDIDATE_REF": cand,
		"SIDE_EFFECT_FILE":   filepath.Join(dir, "side"),
	}
	r := runShim(t, env, "never-recorded-input", runner)
	if r.exit != exitMiss {
		t.Fatalf("miss exit=%d want %d (stderr=%s)", r.exit, exitMiss, r.stderr)
	}
	if !strings.Contains(r.stderr, "cassette miss") {
		t.Fatalf("stderr should name the miss: %s", r.stderr)
	}
}

func TestPassthroughNoCassette(t *testing.T) {
	dir := t.TempDir()
	runner := writeFakeRunner(t, dir)
	cand := writeCandidate(t, dir, "cand.md", "skill-v1")
	side := filepath.Join(dir, "side-effect")

	env := map[string]string{ // XRR_MODE unset
		"EVOL_CANDIDATE_REF": cand,
		"SIDE_EFFECT_FILE":   side,
	}
	r := runShim(t, env, "input", runner)
	if r.exit != 0 || r.stdout != "cand=skill-v1 input=input" {
		t.Fatalf("passthrough exit=%d stdout=%q stderr=%s", r.exit, r.stdout, r.stderr)
	}
	if _, err := os.Stat(side); err != nil {
		t.Fatal("passthrough must spawn")
	}
	if n := len(cassetteFiles(t, dir, ".req.yaml")); n != 0 {
		t.Fatalf("passthrough must not write cassettes, found %d", n)
	}
}

func TestMissingCandidateRef(t *testing.T) {
	dir := t.TempDir()
	runner := writeFakeRunner(t, dir)
	r := runShim(t, map[string]string{"SIDE_EFFECT_FILE": filepath.Join(dir, "s")}, "input", runner)
	if r.exit != exitConfig {
		t.Fatalf("exit=%d want %d", r.exit, exitConfig)
	}
	if !strings.Contains(r.stderr, "EVOL_CANDIDATE_REF") {
		t.Fatalf("stderr should name the missing var: %s", r.stderr)
	}
}

func TestChildExitPropagation(t *testing.T) {
	dir := t.TempDir()
	runner := writeFakeRunner(t, dir)
	cand := writeCandidate(t, dir, "cand.md", "skill-v1")
	cassettes := filepath.Join(dir, "cassettes")

	env := map[string]string{
		"XRR_MODE":           "record",
		"XRR_CASSETTE_DIR":   cassettes,
		"EVOL_CANDIDATE_REF": cand,
		"SIDE_EFFECT_FILE":   filepath.Join(dir, "side"),
		"FAKE_EXIT":          "7",
	}
	if r := runShim(t, env, "input", runner); r.exit != 7 {
		t.Fatalf("record exit=%d want 7", r.exit)
	}
	env["XRR_MODE"] = "replay"
	r := runShim(t, env, "input", runner)
	if r.exit != 7 {
		t.Fatalf("replay exit=%d want 7 (stderr=%s)", r.exit, r.stderr)
	}
	if r.stdout != "cand=skill-v1 input=input" {
		t.Fatalf("replay stdout=%q", r.stdout)
	}
}
