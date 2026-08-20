package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeConfigWithAudit is writeFakeConfig plus an audit port bound
// to the same fake adapter.
func writeFakeConfigWithAudit(t *testing.T) string {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("..", "internal", "engine", "testdata", "fake.py"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "evol.yaml")
	cfg := `
ports:
  artifactstore: {cmd: [python3, ` + fake + `]}
  generator: {cmd: [python3, ` + fake + `]}
  executor: {cmd: [python3, ` + fake + `]}
  corpus: {cmd: [python3, ` + fake + `]}
  scorer: {cmd: [python3, ` + fake + `]}
  audit: {cmd: [python3, ` + fake + `]}
thresholds: {delta: 0.05, trials: 1}
budget: {generations: 1, max_candidates: 1}
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunsListJSON(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	out, _, err := execRoot(t,
		"runs", "list", "--config", writeFakeConfigWithAudit(t),
		"--subject", "skills/fake", "--limit", "5", "--format", "json")
	if err != nil {
		t.Fatalf("runs list: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("bad json %q: %v", out, err)
	}
	if len(rows) != 1 || rows[0]["run_id"] != "r-newest" {
		t.Fatalf("rows: %v", rows)
	}
}

func TestRunsListTable(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	out, _, err := execRoot(t,
		"runs", "list", "--config", writeFakeConfigWithAudit(t))
	if err != nil {
		t.Fatalf("runs list: %v", err)
	}
	if !strings.Contains(out, "RUN-ID") || !strings.Contains(out, "r-newest") {
		t.Fatalf("table output:\n%s", out)
	}
}

func TestRunsShow(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	out, _, err := execRoot(t,
		"runs", "show", "r-newest", "--config", writeFakeConfigWithAudit(t))
	if err != nil {
		t.Fatalf("runs show: %v", err)
	}
	for _, want := range []string{"run:", "r-newest", "outcome:", "promoted", "steps:", "baseline"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestRunsUnconfiguredExitsConfigErrorWithHint(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	// Plain config: no audit port.
	_, errOut, err := execRoot(t,
		"runs", "list", "--config", writeFakeConfig(t))
	if err == nil {
		t.Fatal("want error when audit unconfigured")
	}
	if got := ExitCode(err); got != exitConfigError {
		t.Fatalf("exit: got %d want %d", got, exitConfigError)
	}
	if !strings.Contains(errOut, "audit-tlc") || !strings.Contains(errOut, "audit-fs") {
		t.Fatalf("hint must name both adapters:\n%s", errOut)
	}
}
