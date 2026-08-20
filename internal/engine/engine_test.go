package engine

import (
	"bytes"
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

// scoresPerCandidate returns the per-candidate score counts across all
// recorded generations.
func scoresPerCandidate(records []map[string]any) []int {
	var out []int
	for _, rec := range records {
		cands, _ := rec["candidates"].([]any)
		for _, c := range cands {
			m, _ := c.(map[string]any)
			scores, _ := m["scores"].([]any)
			out = append(out, len(scores))
		}
	}
	return out
}

func TestCorrectionsMergeIntoGatingPool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")
	t.Setenv("EVOL_FAKE_CORRECTIONS", "ok")

	res, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Accepted {
		t.Fatal("candidate should still be promoted with corrections merged")
	}

	// Pool = case-1 (golden) + corr-1 (correction). The duplicate
	// case-1 correction is deduped; corr-train is the wrong split.
	got := scoresPerCandidate(readRecords(t, dir))
	if len(got) != 1 || got[0] != 2 {
		t.Errorf("scores per candidate = %v, want [2] (golden + merged correction)", got)
	}
}

func TestCorrectionsErrorDegrades(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")
	t.Setenv("EVOL_FAKE_CORRECTIONS", "error")

	res, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake")
	if err != nil {
		t.Fatalf("Run must degrade when corrections action errors: %v", err)
	}
	if !res.Accepted {
		t.Error("loop should still promote without corrections")
	}
	got := scoresPerCandidate(readRecords(t, dir))
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("scores per candidate = %v, want [1] (golden case only)", got)
	}
}

func TestAcceptedRecordCarriesFixtures(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	cfg := fakeConfig(t, false)
	cfg.FixturesDir = ".evol/cassettes"
	if _, err := New(cfg).Run(context.Background(), "skills/fake"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	records := readRecords(t, dir)
	cands, _ := records[len(records)-1]["candidates"].([]any)
	m, _ := cands[0].(map[string]any)
	fx, ok := m["fixtures"].(map[string]any)
	if !ok {
		t.Fatalf("accepted record carries no fixtures: %v", m)
	}
	if fx["cassette_dir"] != ".evol/cassettes" {
		t.Errorf("fixtures.cassette_dir = %v, want .evol/cassettes", fx["cassette_dir"])
	}
}

func TestFixturesOmittedWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	if _, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	records := readRecords(t, dir)
	cands, _ := records[len(records)-1]["candidates"].([]any)
	m, _ := cands[0].(map[string]any)
	if _, present := m["fixtures"]; present {
		t.Errorf("fixtures must be omitted when fixtures_dir is unset: %v", m)
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

func executorEnvs(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls.jsonl")) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("fake adapter recorded no calls: %v", err)
	}
	var envs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call struct {
			Port   string         `json:"port"`
			Action string         `json:"action"`
			Env    map[string]any `json:"env"`
		}
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatalf("bad call line %q: %v", line, err)
		}
		if call.Port == "executor" && call.Action == "run" {
			envs = append(envs, call.Env)
		}
	}
	if len(envs) == 0 {
		t.Fatal("no executor runs observed")
	}
	return envs
}

func TestExecutorProviderForwarded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	cfg := fakeConfig(t, false)
	cfg.ExecutorProvider = "ollama://llama3.2:3b?base_url=http://127.0.0.1:11500"
	if _, err := New(cfg).Run(context.Background(), "skills/fake"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, env := range executorEnvs(t, dir) {
		if got, _ := env["provider"].(string); got != cfg.ExecutorProvider {
			t.Errorf("executor env.provider = %q, want %q", got, cfg.ExecutorProvider)
		}
	}
}

func TestExecutorProviderOmittedByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	if _, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, env := range executorEnvs(t, dir) {
		if _, present := env["provider"]; present {
			t.Errorf("executor env should omit provider when unconfigured, got %v", env)
		}
	}
}

// providerOf pulls candidate entries (id, provider, verdict) from the
// recorded generations, flattened in record order.
func recordedEntries(t *testing.T, dir string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, rec := range readRecords(t, dir) {
		gen, _ := rec["generation"].(map[string]any)
		cands, _ := rec["candidates"].([]any)
		for _, c := range cands {
			m, _ := c.(map[string]any)
			m["_gen"] = gen["number"]
			out = append(out, m)
		}
	}
	return out
}

func TestProviderSweepRecordsEvidenceAndGatesOnPrimary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")
	// The secondary provider is scored terribly — if the gate ever
	// looked at it, nothing would be promoted.
	t.Setenv("EVOL_FAKE_PENALIZE_PROV", "prov-b")

	cfg := fakeConfig(t, false)
	cfg.ExecutorProviders = []string{"prov-a", "prov-b"}

	res, err := New(cfg).Run(context.Background(), "skills/fake")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Accepted {
		t.Fatal("candidate must be promoted on the primary provider despite terrible secondary scores")
	}

	entries := recordedEntries(t, dir)
	var baselineEvidence, candEvidence, primary int
	for _, e := range entries {
		verdict, _ := e["verdict"].(string)
		provider, _ := e["provider"].(string)
		id, _ := e["id"].(string)
		gen, _ := e["_gen"].(float64)
		switch {
		case verdict == "evidence" && id == "baseline":
			baselineEvidence++
			if provider != "prov-b" || gen != 0 {
				t.Errorf("baseline evidence = provider %q gen %v, want prov-b gen 0", provider, gen)
			}
		case verdict == "evidence":
			candEvidence++
			if provider != "prov-b" {
				t.Errorf("candidate evidence provider = %q, want prov-b", provider)
			}
			scores, _ := e["scores"].([]any)
			m, _ := scores[0].(map[string]any)
			if v, _ := m["score"].(float64); v != 0.1 {
				t.Errorf("penalized evidence score = %v, want 0.1", v)
			}
		case verdict == VerdictAccepted:
			primary++
			if provider != "prov-a" {
				t.Errorf("accepted provider = %q, want primary prov-a", provider)
			}
		}
	}
	if baselineEvidence != 1 || candEvidence != 1 || primary != 1 {
		t.Errorf("entries = baseline-evidence %d, cand-evidence %d, accepted %d; want 1/1/1",
			baselineEvidence, candEvidence, primary)
	}

	// Both providers must have reached the executor.
	seen := map[string]bool{}
	for _, env := range executorEnvs(t, dir) {
		p, _ := env["provider"].(string)
		seen[p] = true
	}
	if !seen["prov-a"] || !seen["prov-b"] {
		t.Errorf("executor providers seen = %v, want prov-a and prov-b", seen)
	}
}

func TestSingleProviderRecordsNoEvidence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	cfg := fakeConfig(t, false)
	cfg.ExecutorProvider = "prov-a"
	if _, err := New(cfg).Run(context.Background(), "skills/fake"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, e := range recordedEntries(t, dir) {
		if v, _ := e["verdict"].(string); v == "evidence" {
			t.Errorf("single-provider run recorded evidence row: %v", e)
		}
		if g, _ := e["_gen"].(float64); g == 0 {
			t.Errorf("single-provider run recorded a generation-0 row: %v", e)
		}
	}
}

func TestSignificanceAcceptsClearImprovement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")
	t.Setenv("EVOL_FAKE_CASES", "8")

	res, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Accepted {
		t.Fatal("uniform +0.4 improvement over 8 cases must be promoted")
	}
	if res.SigP == nil {
		t.Fatal("sig_p must be reported when significance ran")
	}
	if *res.SigP > 0.01 {
		t.Errorf("sig_p = %v, want tiny for a uniform improvement", *res.SigP)
	}
	entries := recordedEntries(t, dir)
	rationale, _ := entries[0]["rationale"].(string)
	if !strings.Contains(rationale, "p=") {
		t.Errorf("accepted rationale carries no p-value: %q", rationale)
	}
}

func TestSignificanceRejectsNoisyImprovement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")
	t.Setenv("EVOL_FAKE_CASES", "8")
	t.Setenv("EVOL_FAKE_NOISY", "1")

	res, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake")
	if !errors.Is(err, ErrNoImprovement) {
		t.Fatalf("err = %v, want ErrNoImprovement (mean clears delta, p does not)", err)
	}
	if res.Accepted {
		t.Fatal("noisy improvement must not be promoted")
	}
	entries := recordedEntries(t, dir)
	rationale, _ := entries[0]["rationale"].(string)
	if !strings.Contains(rationale, "not significant") {
		t.Errorf("rejection rationale = %q, want a not-significant explanation", rationale)
	}
}

func TestSignificanceSmallSampleFallsBackToMean(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	var log bytes.Buffer
	eng := New(fakeConfig(t, false)) // 1 case < sigMinPairs
	eng.Log = &log

	res, err := eng.Run(context.Background(), "skills/fake")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Accepted {
		t.Fatal("small-sample runs must still gate on mean")
	}
	if res.SigP != nil {
		t.Errorf("sig_p = %v, want nil below the pair floor", *res.SigP)
	}
	if !strings.Contains(log.String(), "significance disabled") {
		t.Errorf("log = %q, want a significance-disabled warning", log.String())
	}
}

// TestRecordCarriesStrategyAndScoreValues pins the corpus record
// payload: candidate rows must carry the generator strategy (tabu keeps
// its strategy dimension) and per-case score values with case ids.
// Regression: strategy was silently dropped before the field existed on
// CandidateOutcome; score values were once suspected missing (they never
// were — this test keeps it that way).
func TestRecordCarriesStrategyAndScoreValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	if _, err := New(fakeConfig(t, false)).Run(context.Background(), "skills/fake"); err != nil {
		t.Fatal(err)
	}

	var checked bool
	for _, rec := range readRecords(t, dir) {
		cands, _ := rec["candidates"].([]any)
		for _, c := range cands {
			m, _ := c.(map[string]any)
			if m["verdict"] != VerdictAccepted {
				continue
			}
			checked = true
			if got, _ := m["strategy"].(string); got != "tighten" {
				t.Errorf("accepted row strategy = %q, want %q", got, "tighten")
			}
			scores, _ := m["scores"].([]any)
			if len(scores) == 0 {
				t.Fatal("accepted row carries no scores")
			}
			s0, _ := scores[0].(map[string]any)
			if id, _ := s0["case_id"].(string); id == "" {
				t.Errorf("score entry missing case_id: %v", s0)
			}
			if v, ok := s0["score"].(float64); !ok || v <= 0 {
				t.Errorf("score entry value not persisted: %v", s0)
			}
		}
	}
	if !checked {
		t.Fatal("no accepted candidate row found in corpus records")
	}
}

func synthConfig(t *testing.T, withKB bool) Config {
	cfg := fakeConfig(t, withKB)
	cfg.Ports.CaseGen.Cmd = cfg.Ports.Corpus.Cmd // same fake adapter
	return cfg
}

func TestSynthesizeQuarantinesGroundedCases(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_DUP", "1")

	res, err := New(synthConfig(t, true)).SynthesizeCases(context.Background(), "skills/fake", 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Generated != 2 || res.Added != 1 || res.Duplicates != 1 || !res.Quarantined {
		t.Fatalf("result = %+v, want generated 2 / added 1 / dup 1 / quarantined", res)
	}
	if len(res.Cases) != 2 || !strings.HasPrefix(res.Cases[0].ID, "syn-") {
		t.Fatalf("case previews = %+v", res.Cases)
	}

	// The add-cases payload must mark every case quarantined + synthetic.
	data, err := os.ReadFile(filepath.Join(dir, "added.jsonl")) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	var added map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &added); err != nil {
		t.Fatal(err)
	}
	for _, c := range added["cases"].([]any) {
		m := c.(map[string]any)
		if m["quarantined"] != true || m["source"] != "synthetic" {
			t.Fatalf("intake case not quarantined synthetic: %v", m)
		}
	}
}

func TestSynthesizeRefusesWithoutKnowledge(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_KB", "unavailable")

	_, err := New(synthConfig(t, true)).SynthesizeCases(context.Background(), "skills/fake", 2)
	if !errors.Is(err, ErrNoKnowledge) {
		t.Fatalf("err = %v, want ErrNoKnowledge", err)
	}
}

func TestSynthesizeWithoutCaseGenPortIsConfigError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	_, err := New(fakeConfig(t, true)).SynthesizeCases(context.Background(), "skills/fake", 2)
	if !errors.Is(err, ErrNoCaseGen) {
		t.Fatalf("err = %v, want ErrNoCaseGen", err)
	}
}

func TestSynthesizeDryAdapterIsNotError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_SYNTH", "dry")

	res, err := New(synthConfig(t, true)).SynthesizeCases(context.Background(), "skills/fake", 2)
	if err != nil || res.Generated != 0 || res.Added != 0 {
		t.Fatalf("res=%+v err=%v, want clean dry result", res, err)
	}
}
