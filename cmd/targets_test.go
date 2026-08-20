package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeConfig writes an evol.yaml whose every port is the engine
// test suite's fake adapter, and leaves artifact unset.
func writeFakeConfig(t *testing.T) string {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("..", "internal", "engine", "testdata", "fake.py"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fake); err != nil {
		t.Fatalf("fake adapter missing: %v", err)
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
thresholds: {delta: 0.05, trials: 1}
budget: {generations: 1, max_candidates: 1}
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTargetsJSON(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")

	out, _, err := execRoot(t,
		"targets", "--config", writeFakeConfig(t), "--format", "json")
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	var rows []struct {
		Ref          string   `json:"ref"`
		Kind         string   `json:"kind"`
		Generations  int      `json:"generations"`
		LastBest     *float64 `json:"last_best_score"`
		NeverEvolved bool     `json:"never_evolved"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("targets output is not JSON: %v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0].Ref != "skills/fake" || rows[0].Kind != "skill" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Generations != 2 || rows[0].LastBest == nil || rows[0].NeverEvolved {
		t.Errorf("history row = %+v, want 2 generations with score", rows[0])
	}
	if !rows[1].NeverEvolved {
		t.Errorf("row %s should be never-evolved", rows[1].Ref)
	}
}

func TestTargetsTable(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")

	out, _, err := execRoot(t, "targets", "--config", writeFakeConfig(t))
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	for _, want := range []string{"REF", "skills/fake", "0.5500", "never evolved"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRunSelectsTargetWhenNoArtifact(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")
	t.Setenv("EVOL_FAKE_GOOD", "1")

	// skills/fake has history; skills/other is never-run → default
	// policy must pick skills/other and feed it into the loop.
	out, errOut, err := execRoot(t,
		"run", "--config", writeFakeConfig(t), "--format", "json")
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, errOut)
	}
	if !strings.Contains(errOut, "selected skills/other (policy never-run)") {
		t.Errorf("stderr missing selection line:\n%s", errOut)
	}
	var res struct {
		ArtifactRef string `json:"artifact_ref"`
		Accepted    bool   `json:"accepted"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("run output not JSON: %v\n%s", err, out)
	}
	if res.ArtifactRef != "skills/other" || !res.Accepted {
		t.Errorf("result = %+v, want accepted run on skills/other", res)
	}
}

func TestRunSelectRejectsBogusPolicy(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())

	_, errOut, err := execRoot(t,
		"run", "--config", writeFakeConfig(t), "--select", "bogus")
	if err == nil {
		t.Fatal("want error for bogus policy")
	}
	if ExitCode(err) != exitConfigError {
		t.Errorf("exit = %d, want %d", ExitCode(err), exitConfigError)
	}
	if !strings.Contains(errOut, "unknown selection policy") {
		t.Errorf("stderr = %s", errOut)
	}
}
