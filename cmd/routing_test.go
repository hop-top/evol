package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture rows follow the corpus-fs generations.jsonl schema exactly
// (one candidate per line; evidence rows carry verdict "evidence" and a
// provider URI; gated rows may carry the primary provider too). The
// e2e evidence file on main holds single-provider runs only, so sweep
// rows here are authored to the recorded shape.
const fixtureRows = `{"generation":{"artifact_ref":"a/SKILL.md","baseline_version":"v1","number":0},"id":"baseline","scores":[{"case_id":"h01","score":0.6},{"case_id":"h02","score":0.7}],"verdict":"evidence","rationale":"provider sweep evidence; not gated","provider":"ollama://qwen3.6:35b?base_url=http://127.0.0.1:11600"}
{"generation":{"artifact_ref":"a/SKILL.md","baseline_version":"v1","number":1},"id":"cand-01","scores":[{"case_id":"h01","score":0.8},{"case_id":"h02","score":0.9}],"verdict":"accepted","rationale":"gate cleared","provider":"claude://haiku"}
{"generation":{"artifact_ref":"a/SKILL.md","baseline_version":"v1","number":1},"id":"cand-01","scores":[{"case_id":"h01","score":0.5},{"case_id":"h02","score":0.5}],"verdict":"evidence","rationale":"provider sweep evidence; not gated","provider":"ollama://qwen3.6:35b?base_url=http://127.0.0.1:11600"}
{"generation":{"artifact_ref":"a/SKILL.md","baseline_version":"v1","number":1},"id":"cand-02","scores":[{"case_id":"h01","score":0.4}],"verdict":"rejected","rationale":"below gate"}
`

func seedCorpus(t *testing.T, root, ref string) {
	t.Helper()
	sum := sha256.Sum256([]byte(ref))
	dir := filepath.Join(root, hex.EncodeToString(sum[:])[:12])
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "generations.jsonl"), []byte(fixtureRows), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeAdapter writes a script that records its request and answers.
func fakeAdapter(t *testing.T, dir string) (bin, reqFile string) {
	t.Helper()
	reqFile = filepath.Join(dir, "req.json")
	bin = filepath.Join(dir, "fake-routing")
	script := fmt.Sprintf(`#!/bin/sh
cat > %q
printf '{"evol":"1","port":"routing","action":"emit","written":"out.yaml","entries":[{"alias":"haiku-evol","model":"haiku","weight":1.0}]}\n'
`, reqFile)
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture executable
		t.Fatal(err)
	}
	return bin, reqFile
}

func TestRoutingEmitAggregatesPerProvider(t *testing.T) {
	dir := t.TempDir()
	seedCorpus(t, dir, "a/SKILL.md")
	t.Setenv("EVOL_CORPUS_ROOT", dir)
	bin, reqFile := fakeAdapter(t, dir)

	out, errOut, execErr := execRoot(t,
		"routing", "emit", "--artifact", "a/SKILL.md",
		"--adapter", fmt.Sprintf(`[%q]`, bin), "--out", "pool.yaml")
	if execErr != nil {
		t.Fatalf("routing emit failed: %v (stderr %s)", execErr, errOut)
	}

	raw, err := os.ReadFile(reqFile) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Evidence []providerStat `json:"evidence"`
		Output   struct {
			Path string `json:"path"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Evidence) != 2 {
		t.Fatalf("providers = %d, want 2 (row without provider excluded): %+v", len(req.Evidence), req.Evidence)
	}
	byProv := map[string]providerStat{}
	for _, e := range req.Evidence {
		byProv[e.Provider] = e
	}
	oll := byProv["ollama://qwen3.6:35b?base_url=http://127.0.0.1:11600"]
	if oll.N != 2 || !almost(oll.MeanScore, 0.575) { // (0.65+0.5)/2
		t.Errorf("ollama stat = %+v, want n=2 mean 0.575", oll)
	}
	cla := byProv["claude://haiku"]
	if cla.N != 1 || !almost(cla.MeanScore, 0.85) {
		t.Errorf("claude stat = %+v, want n=1 mean 0.85", cla)
	}
	if req.Output.Path != "pool.yaml" {
		t.Errorf("output path = %q", req.Output.Path)
	}
	if !strings.Contains(out, "haiku-evol") {
		t.Errorf("human output missing entries: %q", out)
	}
}

func TestRoutingEmitNoEvidence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", dir)
	_, _, err := execRoot(t, "routing", "emit", "--artifact", "missing/SKILL.md")
	if err == nil || ExitCode(err) != exitConfigError {
		t.Errorf("want config error on missing corpus, got %v", err)
	}
}

func TestRoutingEmitRequiresArtifact(t *testing.T) {
	_, _, err := execRoot(t, "routing", "emit")
	if err == nil || ExitCode(err) != exitConfigError {
		t.Errorf("want config error without --artifact, got %v", err)
	}
}

func almost(got, want float64) bool {
	d := got - want
	return d < 1e-9 && d > -1e-9
}
