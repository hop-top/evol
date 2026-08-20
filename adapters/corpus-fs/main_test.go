package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ref = "skills/commit-style"

func call(t *testing.T, input string) (map[string]json.RawMessage, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(strings.NewReader(input), &out)
	if err != nil {
		return nil, err
	}
	var resp map[string]json.RawMessage
	if uerr := json.Unmarshal(out.Bytes(), &resp); uerr != nil {
		t.Fatalf("response not JSON: %v\n%s", uerr, out.String())
	}
	return resp, nil
}

func seed(t *testing.T, root, file string, lines ...string) {
	t.Helper()
	dir, err := artifactDir(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCasesFilterSplitAndDedup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, casesFile,
		`{"id":"c1","input":"a","expected":"x","split":"train","source":"golden"}`,
		`{"id":"c2","input":"b","expected":"y","split":"holdout","source":"golden"}`,
		`{"id":"c3","input":"b","expected":"y","split":"holdout","source":"synthetic"}`, // dup content
		`not json at all`, // must be skipped, not fatal
	)

	resp, err := call(t, `{"evol":"1","port":"corpus","action":"cases","artifact_ref":"`+ref+`","split":"holdout"}`)
	if err != nil {
		t.Fatal(err)
	}
	var cases []caseEntry
	if err := json.Unmarshal(resp["cases"], &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID != "c2" {
		t.Fatalf("cases = %+v, want only c2", cases)
	}
}

func TestCasesMissingArtifactIsEmpty(t *testing.T) {
	t.Setenv("EVOL_CORPUS_ROOT", t.TempDir())
	resp, err := call(t, `{"evol":"1","port":"corpus","action":"cases","artifact_ref":"never/recorded"}`)
	if err != nil {
		t.Fatal(err)
	}
	var cases []caseEntry
	if err := json.Unmarshal(resp["cases"], &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 0 {
		t.Fatalf("want empty cases, got %+v", cases)
	}
}

func TestRecordCreatesDirAndAppends(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)

	record := `{"evol":"1","port":"corpus","action":"record",
	 "generation":{"artifact_ref":"` + ref + `","baseline_version":"b19","number":1},
	 "candidates":[
	   {"id":"cand-01","scores":[{"case_id":"c1","score":0.4,"reason":"weak"}],"verdict":"rejected","rationale":"below baseline","strategy":"reorder"},
	   {"id":"cand-02","scores":[{"case_id":"c1","score":0.9}],"verdict":"accepted","rationale":"beats baseline","strategy":"tighten"}]}`
	if _, err := call(t, record); err != nil {
		t.Fatal(err) // dir did not exist before this call
	}
	// Second record appends (O_APPEND accumulation).
	record2 := `{"evol":"1","port":"corpus","action":"record",
	 "generation":{"artifact_ref":"` + ref + `","number":2},
	 "candidates":[{"id":"cand-03","verdict":"failed","rationale":"executor timeout","strategy":"reorder"}]}`
	if _, err := call(t, record2); err != nil {
		t.Fatal(err)
	}

	dir, _ := artifactDir(root, ref)
	lines, err := readJSONL[generationLine](filepath.Join(dir, generationsFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("want 3 recorded lines, got %d", len(lines))
	}
	if lines[2].Generation.Number != 2 || lines[2].ID != "cand-03" {
		t.Fatalf("last line = %+v", lines[2])
	}
	refBytes, err := os.ReadFile(filepath.Join(dir, refFile)) //nolint:gosec // test reads its own tempdir
	if err != nil || !strings.Contains(string(refBytes), ref) {
		t.Fatalf("ref breadcrumb missing: %v %q", err, refBytes)
	}
}

func TestRecordRoundTripsFixtures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)

	req := `{"evol":"1","port":"corpus","action":"record",` +
		`"generation":{"artifact_ref":"` + ref + `","baseline_version":"v1","number":1},` +
		`"candidates":[{"id":"cand-1","verdict":"accepted","rationale":"gate met",` +
		`"fixtures":{"cassette_dir":".evol/cassettes"}}]}`
	if _, err := call(t, req); err != nil {
		t.Fatal(err)
	}

	dir, err := artifactDir(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, generationsFile)) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	var got generationLine
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("stored line not JSON: %v\n%s", err, line)
	}
	if !strings.Contains(string(got.Fixtures), `.evol/cassettes`) {
		t.Errorf("fixtures did not round-trip: %s", line)
	}

	// A record without fixtures must not grow an empty field.
	req = `{"evol":"1","port":"corpus","action":"record",` +
		`"generation":{"artifact_ref":"` + ref + `","baseline_version":"v1","number":2},` +
		`"candidates":[{"id":"cand-2","verdict":"rejected","rationale":"below gate"}]}`
	if _, err := call(t, req); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(dir, generationsFile)) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 stored lines, got %d", len(lines))
	}
	if strings.Contains(lines[1], "fixtures") {
		t.Errorf("fixtures key must be omitted when absent: %s", lines[1])
	}
}

func TestTabuReturnsRejectsAndFailsDeduped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, generationsFile,
		`{"generation":{"artifact_ref":"`+ref+`","number":1},"id":"c1","verdict":"rejected","rationale":"holdout regression","strategy":"reorder"}`,
		`{"generation":{"artifact_ref":"`+ref+`","number":1},"id":"c2","verdict":"accepted","rationale":"win","strategy":"tighten"}`,
		`{"generation":{"artifact_ref":"`+ref+`","number":2},"id":"c3","verdict":"rejected","rationale":"holdout regression","strategy":"reorder"}`, // dup (strategy, rationale)
		`{"generation":{"artifact_ref":"`+ref+`","number":2},"id":"c4","verdict":"failed","rationale":"executor timeout","strategy":"add-example"}`,
	)

	resp, err := call(t, `{"evol":"1","port":"corpus","action":"tabu","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var entries []tabuEntry
	if err := json.Unmarshal(resp["entries"], &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 tabu entries (dedup + no accepted), got %+v", entries)
	}
	if entries[0].Strategy != "reorder" || entries[1].Verdict != "failed" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestCorrectionsForceSource(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, correctionsFile,
		`{"id":"corr-1","input":"revert msg","expected":"revert: prefix","split":"train"}`,
	)

	resp, err := call(t, `{"evol":"1","port":"corpus","action":"corrections","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var cases []caseEntry
	if err := json.Unmarshal(resp["cases"], &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Source != "correction" {
		t.Fatalf("cases = %+v", cases)
	}
}

func TestAdapterErrors(t *testing.T) {
	t.Setenv("EVOL_CORPUS_ROOT", t.TempDir())
	for name, input := range map[string]string{
		"wrong version": `{"evol":"0","port":"corpus","action":"cases","artifact_ref":"a"}`,
		"wrong port":    `{"evol":"1","port":"scorer","action":"cases","artifact_ref":"a"}`,
		"bad action":    `{"evol":"1","port":"corpus","action":"nope","artifact_ref":"a"}`,
		"no ref":        `{"evol":"1","port":"corpus","action":"tabu"}`,
		"not json":      `{{{`,
	} {
		if _, err := call(t, input); err == nil {
			t.Fatalf("%s: want error", name)
		}
	}

	// Missing root env is an adapter error too.
	t.Setenv("EVOL_CORPUS_ROOT", "")
	if _, err := call(t, `{"evol":"1","port":"corpus","action":"cases","artifact_ref":"a"}`); err == nil {
		t.Fatal("missing EVOL_CORPUS_ROOT: want error")
	}
}

func TestHistorySummarizesGenerations(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, generationsFile,
		// generation 1: two candidates; cand-2's mean (0.6) is best.
		`{"generation":{"artifact_ref":"`+ref+`","number":1},"id":"cand-1","scores":[{"score":0.2},{"score":0.4}],"verdict":"rejected"}`,
		`{"generation":{"artifact_ref":"`+ref+`","number":1},"id":"cand-2","scores":[{"score":0.6}],"verdict":"rejected"}`,
		// generation 2: single accepted candidate, mean 0.8.
		`{"generation":{"artifact_ref":"`+ref+`","number":2},"id":"cand-3","scores":[{"score":0.7},{"score":0.9}],"verdict":"accepted"}`,
	)

	resp, err := call(t, `{"evol":"1","port":"corpus","action":"history","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var gens []struct {
		Generation int     `json:"generation"`
		BestScore  float64 `json:"best_score"`
		Verdict    string  `json:"verdict"`
	}
	if err := json.Unmarshal(resp["generations"], &gens); err != nil {
		t.Fatal(err)
	}
	if len(gens) != 2 {
		t.Fatalf("generations = %d, want 2", len(gens))
	}
	if gens[0].Generation != 1 || gens[0].BestScore != 0.6 || gens[0].Verdict != "rejected" {
		t.Errorf("gen1 = %+v, want best 0.6 rejected", gens[0])
	}
	if gens[1].Generation != 2 || gens[1].BestScore != 0.8 || gens[1].Verdict != "accepted" {
		t.Errorf("gen2 = %+v, want best 0.8 accepted", gens[1])
	}
}

func TestHistoryEmptyForUnknownArtifact(t *testing.T) {
	t.Setenv("EVOL_CORPUS_ROOT", t.TempDir())

	resp, err := call(t, `{"evol":"1","port":"corpus","action":"history","artifact_ref":"never/recorded"}`)
	if err != nil {
		t.Fatal(err)
	}
	var gens []any
	if err := json.Unmarshal(resp["generations"], &gens); err != nil {
		t.Fatal(err)
	}
	if len(gens) != 0 {
		t.Errorf("generations = %v, want empty", gens)
	}
}

func TestRecordRoundTripsProvider(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)

	if _, err := call(t, `{"evol":"1","port":"corpus","action":"record",
		"generation":{"artifact_ref":"`+ref+`","baseline_version":"v1","number":1},
		"candidates":[
		  {"id":"cand-1","scores":[{"score":0.9}],"verdict":"accepted","rationale":"ok","provider":"prov-a"},
		  {"id":"cand-1","scores":[{"score":0.1}],"verdict":"evidence","rationale":"sweep","provider":"prov-b"},
		  {"id":"cand-2","scores":[{"score":0.3}],"verdict":"rejected","rationale":"below gate"}
		]}`); err != nil {
		t.Fatalf("record: %v", err)
	}

	dir, _ := artifactDir(root, ref)
	data, err := os.ReadFile(filepath.Join(dir, generationsFile)) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("recorded %d lines, want 3", len(lines))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["provider"] != "prov-a" {
		t.Errorf("provider = %v, want prov-a round-tripped", first["provider"])
	}
	var third map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil {
		t.Fatal(err)
	}
	if _, present := third["provider"]; present {
		t.Error("provider must be omitted when absent from the request")
	}
}

func TestTabuAndHistoryIgnoreEvidenceRows(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, generationsFile,
		`{"generation":{"artifact_ref":"`+ref+`","number":0},"id":"baseline","scores":[{"score":0.95}],"verdict":"evidence","rationale":"sweep","provider":"prov-b"}`,
		`{"generation":{"artifact_ref":"`+ref+`","number":1},"id":"cand-1","scores":[{"score":0.4}],"verdict":"rejected","rationale":"below gate","strategy":"tighten"}`,
		`{"generation":{"artifact_ref":"`+ref+`","number":1},"id":"cand-1","scores":[{"score":0.99}],"verdict":"evidence","rationale":"sweep","strategy":"tighten","provider":"prov-b"}`,
	)

	tabuResp, err := call(t, `{"evol":"1","port":"corpus","action":"tabu","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatalf("tabu: %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(tabuResp["entries"], &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0]["verdict"] != "rejected" {
		t.Errorf("tabu entries = %v, want only the rejected row", entries)
	}

	histResp, err := call(t, `{"evol":"1","port":"corpus","action":"history","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var gens []map[string]any
	if err := json.Unmarshal(histResp["generations"], &gens); err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 {
		t.Fatalf("history = %v, want a single generation (evidence rows skipped)", gens)
	}
	if gens[0]["best_score"].(float64) != 0.4 || gens[0]["verdict"] != "rejected" {
		t.Errorf("history best = %v, want the rejected 0.4 — a 0.99 evidence row must not masquerade as progress", gens[0])
	}
}

func TestAddCasesDedupAndQuarantineExclusion(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, casesFile,
		`{"id":"g1","input":"existing input","expected":"existing expected","split":"train","source":"golden"}`)

	add := `{"evol":"1","port":"corpus","action":"add-cases","artifact_ref":"` + ref + `",
	 "cases":[
	   {"input":"existing input","expected":"existing expected","split":"train","source":"synthetic","quarantined":true},
	   {"input":"new synthesized input","expected":"new expected","split":"train","source":"synthetic","quarantined":true},
	   {"input":"new synthesized input","expected":"new expected","split":"train","source":"synthetic","quarantined":true}]}`
	resp, err := call(t, add)
	if err != nil {
		t.Fatal(err)
	}
	var added, dups int
	var ids []string
	mustUnmarshal(t, resp["added"], &added)
	mustUnmarshal(t, resp["duplicates"], &dups)
	mustUnmarshal(t, resp["ids"], &ids)
	if added != 1 || dups != 2 {
		t.Fatalf("added=%d duplicates=%d, want 1/2 (content dedup vs existing + within request)", added, dups)
	}
	if len(ids) != 1 || !strings.HasPrefix(ids[0], "syn-") {
		t.Fatalf("ids = %v, want one deterministic syn- id", ids)
	}

	// Quarantined case must NOT be served by `cases`...
	resp, err = call(t, `{"evol":"1","port":"corpus","action":"cases","artifact_ref":"`+ref+`","split":"train"}`)
	if err != nil {
		t.Fatal(err)
	}
	var cases []caseEntry
	mustUnmarshal(t, resp["cases"], &cases)
	if len(cases) != 1 || cases[0].ID != "g1" {
		t.Fatalf("cases = %+v, want only the golden case while quarantined", cases)
	}

	// ...until promoted.
	resp, err = call(t, `{"evol":"1","port":"corpus","action":"promote-cases","artifact_ref":"`+ref+`",
	 "ids":["`+ids[0]+`","nope-404"]}`)
	if err != nil {
		t.Fatal(err)
	}
	var promoted int
	var missing []string
	mustUnmarshal(t, resp["promoted"], &promoted)
	mustUnmarshal(t, resp["missing"], &missing)
	if promoted != 1 || len(missing) != 1 || missing[0] != "nope-404" {
		t.Fatalf("promoted=%d missing=%v, want 1/[nope-404]", promoted, missing)
	}
	resp, err = call(t, `{"evol":"1","port":"corpus","action":"cases","artifact_ref":"`+ref+`","split":"train"}`)
	if err != nil {
		t.Fatal(err)
	}
	mustUnmarshal(t, resp["cases"], &cases)
	if len(cases) != 2 {
		t.Fatalf("cases after promote = %d, want 2", len(cases))
	}

	// Promote is idempotent: re-promoting reports 0.
	resp, err = call(t, `{"evol":"1","port":"corpus","action":"promote-cases","artifact_ref":"`+ref+`","ids":["`+ids[0]+`"]}`)
	if err != nil {
		t.Fatal(err)
	}
	mustUnmarshal(t, resp["promoted"], &promoted)
	if promoted != 0 {
		t.Fatalf("re-promote promoted=%d, want 0", promoted)
	}
}

func TestAddCasesRequiresInput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	_, err := call(t, `{"evol":"1","port":"corpus","action":"add-cases","artifact_ref":"`+ref+`",
	 "cases":[{"expected":"only expected"}]}`)
	if err == nil || !strings.Contains(err.Error(), "input is required") {
		t.Fatalf("err = %v, want input-required adapter error", err)
	}
}

func mustUnmarshal(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	if raw == nil {
		t.Fatal("missing response field")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatal(err)
	}
}

func TestCasesIncludeQuarantined(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, casesFile,
		`{"id":"c1","input":"a","expected":"x","split":"train","source":"golden"}`,
		`{"id":"q1","input":"b","expected":"y","split":"train","source":"synthetic","quarantined":true}`,
	)

	// Default: quarantined excluded (gating semantics preserved).
	resp, err := call(t, `{"evol":"1","port":"corpus","action":"cases","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var cases []caseEntry
	if err := json.Unmarshal(resp["cases"], &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID != "c1" {
		t.Fatalf("default cases = %+v, want only c1", cases)
	}

	// include_quarantined: both served, quarantine flag visible.
	resp, err = call(t, `{"evol":"1","port":"corpus","action":"cases","artifact_ref":"`+ref+`","include_quarantined":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(resp["cases"], &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("include_quarantined cases = %+v, want 2", cases)
	}
	var quar *caseEntry
	for i := range cases {
		if cases[i].ID == "q1" {
			quar = &cases[i]
		}
	}
	if quar == nil || !quar.Quarantined {
		t.Fatalf("q1 missing or not flagged quarantined: %+v", cases)
	}
}

func TestAddCorrectionsRoundTripAndDedup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)

	// First write creates the store; source forced, never quarantined.
	resp, err := call(t, `{"evol":"1","port":"corpus","action":"add-corrections","artifact_ref":"`+ref+`",
		"corrections":[{"id":"corr-1","input":"revert commit message","expected":"revert: original subject","split":"train","source":"golden","quarantined":true}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var added int
	if err := json.Unmarshal(resp["added"], &added); err != nil || added != 1 {
		t.Fatalf("added = %s (%v), want 1", resp["added"], err)
	}

	// Served by the corrections action with source correction, unquarantined.
	resp, err = call(t, `{"evol":"1","port":"corpus","action":"corrections","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var cases []caseEntry
	if err := json.Unmarshal(resp["cases"], &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID != "corr-1" || cases[0].Source != "correction" || cases[0].Quarantined {
		t.Fatalf("corrections = %+v, want corr-1/correction/unquarantined", cases)
	}

	// Same id again: duplicate. Same content, new id: duplicate too.
	resp, err = call(t, `{"evol":"1","port":"corpus","action":"add-corrections","artifact_ref":"`+ref+`",
		"corrections":[{"id":"corr-1","input":"different","expected":""},{"id":"corr-2","input":"revert commit message","expected":"revert: original subject"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var dups int
	if err := json.Unmarshal(resp["duplicates"], &dups); err != nil || dups != 2 {
		t.Fatalf("duplicates = %s (%v), want 2", resp["duplicates"], err)
	}

	// Missing id / input are adapter errors.
	if _, err := call(t, `{"evol":"1","port":"corpus","action":"add-corrections","artifact_ref":"`+ref+`","corrections":[{"input":"x"}]}`); err == nil {
		t.Fatal("missing id accepted")
	}
	if _, err := call(t, `{"evol":"1","port":"corpus","action":"add-corrections","artifact_ref":"`+ref+`","corrections":[{"id":"corr-9"}]}`); err == nil {
		t.Fatal("missing input accepted")
	}
}
