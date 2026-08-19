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
