package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeConfig(t *testing.T, withKB bool) Config {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("testdata", "fake.py"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := []string{"python3", fake}

	var cfg Config
	cfg.Ports.ArtifactStore.Cmd = cmd
	cfg.Ports.Generator.Cmd = cmd
	cfg.Ports.Executor.Cmd = cmd
	cfg.Ports.Corpus.Cmd = cmd
	cfg.Ports.Scorer.Cmd = cmd
	if withKB {
		cfg.Ports.KnowledgeBase.Cmd = cmd
	}
	cfg.Thresholds.Delta = 0.1
	cfg.Budget.Generations = 2
	cfg.Budget.MaxCandidates = 2
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func readRecords(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "record.jsonl")) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("corpus record was never written: %v", err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad record line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

func verdicts(records []map[string]any) []string {
	var out []string
	for _, rec := range records {
		cands, _ := rec["candidates"].([]any)
		for _, c := range cands {
			m, _ := c.(map[string]any)
			v, _ := m["verdict"].(string)
			out = append(out, v)
		}
	}
	return out
}

func TestPromotesImprovingCandidate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	res, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Accepted || res.AcceptedID != "cand-1" || res.NewVersion != "v2" {
		t.Errorf("result = %+v, want accepted cand-1 at v2", res)
	}
	if res.BaselineScore != 0.5 || res.BestScore != 0.9 {
		t.Errorf("scores = baseline %.2f best %.2f, want 0.50/0.90",
			res.BaselineScore, res.BestScore)
	}

	// The accepted candidate must be written through the store...
	written, err := os.ReadFile(filepath.Join(dir, "written.json")) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("artifactstore.write never called: %v", err)
	}
	if !strings.Contains(string(written), "IMPROVED") {
		t.Error("written artifact should carry the candidate body")
	}
	// ...and its verdict recorded to the corpus.
	got := verdicts(readRecords(t, dir))
	if len(got) != 1 || got[0] != VerdictAccepted {
		t.Errorf("recorded verdicts = %v, want [accepted]", got)
	}
}

func TestRejectedCandidatesLandInCorpus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "0")

	res, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake")
	if !errors.Is(err, ErrNoImprovement) {
		t.Fatalf("err = %v, want ErrNoImprovement", err)
	}
	if res.Accepted {
		t.Error("nothing should be accepted")
	}
	if res.Generations != 2 {
		t.Errorf("generations = %d, want full budget 2", res.Generations)
	}

	got := verdicts(readRecords(t, dir))
	if len(got) != 2 {
		t.Fatalf("recorded %d verdicts, want one per generation (2): %v", len(got), got)
	}
	for _, v := range got {
		if v != VerdictRejected {
			t.Errorf("verdict = %q, want rejected (write-back on reject is mandatory)", v)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "written.json")); !os.IsNotExist(err) {
		t.Error("artifactstore.write must not run for rejected candidates")
	}
}

func TestKnowledgeBaseUnavailableDegrades(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")
	t.Setenv("EVOL_FAKE_KB", "unavailable")

	res, err := New(fakeConfig(t, true)).Run(context.Background(), "skills/fake")
	if err != nil {
		t.Fatalf("Run must degrade gracefully when KB is unavailable: %v", err)
	}
	if !res.Accepted {
		t.Error("loop should still promote without knowledge")
	}
}

func TestConfigNormalize(t *testing.T) {
	var cfg Config
	if err := cfg.Normalize(); err == nil {
		t.Fatal("want error when required ports are missing")
	}

	cfg = fakeConfig(t, false)
	if cfg.Holdout != "holdout" || cfg.ExecutorMode != "replay" {
		t.Errorf("defaults = %q/%q, want holdout/replay", cfg.Holdout, cfg.ExecutorMode)
	}
	if cfg.Thresholds.Trials != 1 {
		t.Errorf("trials default = %d, want 1", cfg.Thresholds.Trials)
	}
}

func TestStageReassemblesFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path, err := stage(dir, "cand/../1", "name: x", "body text")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("sanitize must keep staging inside %s, got %s", dir, path)
	}
	data, _ := os.ReadFile(path) // #nosec G304 -- test temp dir
	want := "---\nname: x\n---\n\nbody text"
	if string(data) != want {
		t.Errorf("staged doc = %q, want %q", data, want)
	}
}
