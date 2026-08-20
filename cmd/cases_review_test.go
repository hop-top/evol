package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeCorpusOnlyConfig binds only the corpus port, to the review-verb
// fake adapter that logs requests to reqLog.
func writeCorpusOnlyConfig(t *testing.T, reqLog string) string {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("testdata", "fake_corpus.py"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "evol.yaml")
	cfg := `
artifact: skills/demo/SKILL.md
ports:
  corpus: {cmd: [python3, ` + fake + `]}
`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_REQ_LOG", reqLog)
	return path
}

func readReqs(t *testing.T, reqLog string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(reqLog) //nolint:gosec // test-owned t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	var reqs []map[string]any
	for _, line := range splitLines(data) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		reqs = append(reqs, m)
	}
	return reqs
}

func splitLines(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, string(b[start:i]))
			}
			start = i + 1
		}
	}
	return out
}

func TestCasesListDefaultExcludesQuarantined(t *testing.T) {
	reqLog := filepath.Join(t.TempDir(), "reqs.jsonl")
	cfg := writeCorpusOnlyConfig(t, reqLog)

	out, _, err := execRoot(t, "cases", "list", "--config", cfg, "--format", "json")
	if err != nil {
		t.Fatalf("cases list: %v", err)
	}
	var resp struct {
		Cases []reviewCase `json:"cases"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(resp.Cases) != 1 || resp.Cases[0].ID != "c1" {
		t.Fatalf("cases = %+v, want only c1", resp.Cases)
	}
	reqs := readReqs(t, reqLog)
	if _, present := reqs[0]["include_quarantined"]; present {
		t.Fatalf("default request must omit include_quarantined: %v", reqs[0])
	}
}

func TestCasesListQuarantinedOnly(t *testing.T) {
	reqLog := filepath.Join(t.TempDir(), "reqs.jsonl")
	cfg := writeCorpusOnlyConfig(t, reqLog)

	out, _, err := execRoot(t, "cases", "list", "--config", cfg, "--quarantined", "--format", "json")
	if err != nil {
		t.Fatalf("cases list --quarantined: %v", err)
	}
	var resp struct {
		Cases []reviewCase `json:"cases"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(resp.Cases) != 1 || resp.Cases[0].ID != "q1" || !resp.Cases[0].Quarantined {
		t.Fatalf("cases = %+v, want only quarantined q1", resp.Cases)
	}
	reqs := readReqs(t, reqLog)
	if v, _ := reqs[0]["include_quarantined"].(bool); !v {
		t.Fatalf("request must set include_quarantined: %v", reqs[0])
	}
}

func TestCasesListAllTable(t *testing.T) {
	reqLog := filepath.Join(t.TempDir(), "reqs.jsonl")
	cfg := writeCorpusOnlyConfig(t, reqLog)

	out, _, err := execRoot(t, "cases", "list", "--config", cfg, "--all")
	if err != nil {
		t.Fatalf("cases list --all: %v", err)
	}
	for _, want := range []string{"ID", "QUAR", "c1", "q1", "yes"} {
		if !contains(out, want) {
			t.Fatalf("table missing %q:\n%s", want, out)
		}
	}
}

func TestCasesCorrectPayloadAndValidation(t *testing.T) {
	reqLog := filepath.Join(t.TempDir(), "reqs.jsonl")
	cfg := writeCorpusOnlyConfig(t, reqLog)

	_, _, err := execRoot(t, "cases", "correct", "--config", cfg,
		"--case-id", "corr-42", "--input", "revert message",
		"--expected", "revert: original subject", "--split", "holdout",
		"--format", "json")
	if err != nil {
		t.Fatalf("cases correct: %v", err)
	}
	reqs := readReqs(t, reqLog)
	corr := reqs[0]["corrections"].([]any)[0].(map[string]any)
	if corr["id"] != "corr-42" || corr["input"] != "revert message" ||
		corr["expected"] != "revert: original subject" || corr["split"] != "holdout" {
		t.Fatalf("payload = %v", corr)
	}

	// Validation failures are config errors before any adapter call.
	if _, _, err := execRoot(t, "cases", "correct", "--config", cfg, "--input", "x"); err == nil {
		t.Fatal("missing --case-id accepted")
	}
	if _, _, err := execRoot(t, "cases", "correct", "--config", cfg, "--case-id", "a", "--input", "x", "--split", "weird"); err == nil {
		t.Fatal("bad --split accepted")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
