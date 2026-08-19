package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), errBuf.String(), err
}

func writeConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "evol.yaml")
	cfg := `
artifact: skills/fake
ports:
  artifactstore: {cmd: [fake]}
  generator: {cmd: [fake]}
  executor: {cmd: [fake]}
  corpus: {cmd: [fake]}
  scorer: {cmd: [fake]}
thresholds: {delta: 0.05, trials: 1}
budget: {generations: 2, max_candidates: 3}
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunDryRunPrintsPlan(t *testing.T) {
	out, _, err := execRoot(t,
		"run", "--config", writeConfig(t), "--dry-run", "--format", "json")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	var plan struct {
		DryRun   bool   `json:"dry_run"`
		Artifact string `json:"artifact"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, out)
	}
	if !plan.DryRun || plan.Artifact != "skills/fake" {
		t.Errorf("plan = %+v, want dry_run=true artifact=skills/fake", plan)
	}
}

func TestRunMissingConfigExitsConfigError(t *testing.T) {
	_, stderr, err := execRoot(t,
		"run", "--config", filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil {
		t.Fatal("want error for missing config")
	}
	if code := ExitCode(err); code != exitConfigError {
		t.Errorf("exit code = %d, want %d", code, exitConfigError)
	}
	if !strings.Contains(stderr, "evol:") {
		t.Errorf("stderr should carry the diagnostic, got %q", stderr)
	}
}

func TestExitCodeDefaultsToOne(t *testing.T) {
	if code := ExitCode(errors.New("plain")); code != 1 {
		t.Errorf("plain error exit = %d, want 1", code)
	}
	coded := &codedError{code: exitGateFail, err: errors.New("x")}
	if code := ExitCode(coded); code != exitGateFail {
		t.Errorf("coded exit = %d, want %d", code, exitGateFail)
	}
}
