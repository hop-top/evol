package main

import (
	"encoding/json"
	"testing"
)

// TestHistoryCarriesLatestRecordedAt: record rows round-trip their
// recorded_at, and history reports the latest non-evidence stamp per
// generation ("when did this generation happen").
func TestHistoryCarriesLatestRecordedAt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)

	if _, err := call(t, `{"evol":"1","port":"corpus","action":"record",
	 "generation":{"artifact_ref":"`+ref+`","number":1},
	 "candidates":[
	  {"id":"cand-1","scores":[{"score":0.4}],"verdict":"rejected","recorded_at":"2026-08-19T10:00:00Z"},
	  {"id":"cand-2","scores":[{"score":0.6}],"verdict":"rejected","recorded_at":"2026-08-19T11:00:00Z"},
	  {"id":"cand-2","scores":[{"score":0.9}],"verdict":"evidence","provider":"ollama://x","recorded_at":"2026-08-19T23:00:00Z"}]}`); err != nil {
		t.Fatal(err)
	}

	resp, err := call(t, `{"evol":"1","port":"corpus","action":"history","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var gens []struct {
		Generation int     `json:"generation"`
		BestScore  float64 `json:"best_score"`
		RecordedAt string  `json:"recorded_at"`
	}
	if err := json.Unmarshal(resp["generations"], &gens); err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 {
		t.Fatalf("generations = %d, want 1", len(gens))
	}
	// Latest NON-evidence stamp wins: the evidence row's 23:00 stamp must
	// not leak into the generation clock.
	if gens[0].RecordedAt != "2026-08-19T11:00:00Z" {
		t.Fatalf("recorded_at = %q, want the latest non-evidence stamp", gens[0].RecordedAt)
	}
	if gens[0].BestScore != 0.6 {
		t.Fatalf("best = %v, want 0.6", gens[0].BestScore)
	}
}

// Rows recorded before stamps existed keep working: history simply
// omits recorded_at.
func TestHistoryWithoutStampsOmitsRecordedAt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("EVOL_CORPUS_ROOT", root)
	seed(t, root, generationsFile,
		`{"generation":{"artifact_ref":"`+ref+`","number":1},"id":"cand-1","scores":[{"score":0.5}],"verdict":"rejected"}`,
	)
	resp, err := call(t, `{"evol":"1","port":"corpus","action":"history","artifact_ref":"`+ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var gens []map[string]any
	if err := json.Unmarshal(resp["generations"], &gens); err != nil {
		t.Fatal(err)
	}
	if _, present := gens[0]["recorded_at"]; present {
		t.Fatalf("recorded_at should be omitted for unstamped rows: %v", gens[0])
	}
}
