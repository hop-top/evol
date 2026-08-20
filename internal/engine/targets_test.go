package engine

import (
	"context"
	"strings"
	"testing"
)

func f64(v float64) *float64 { return &v }

func TestTargetsJoinsHistory(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")

	rows, err := New(fakeConfig(t, false)).Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Sorted by ref: skills/fake first.
	fake, other := rows[0], rows[1]
	if fake.Ref != "skills/fake" || other.Ref != "skills/other" {
		t.Fatalf("row order = %s, %s", fake.Ref, other.Ref)
	}
	if fake.NeverEvolved || fake.Generations != 2 ||
		fake.LastBest == nil || *fake.LastBest != 0.55 || fake.LastVerdict != "rejected" {
		t.Errorf("fake row = %+v, want 2 gens last 0.55 rejected", fake)
	}
	if !other.NeverEvolved || other.Generations != 0 || other.LastBest != nil {
		t.Errorf("other row = %+v, want never-evolved", other)
	}
}

func TestTargetsDegradesOnHistoryError(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "error")

	rows, err := New(fakeConfig(t, false)).Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, r := range rows {
		if !r.NeverEvolved || r.Note == "" ||
			!strings.Contains(r.Note, "history unavailable") {
			t.Errorf("row %s = %+v, want degraded never-evolved with note", r.Ref, r)
		}
	}
}

func TestSelectTargetPolicies(t *testing.T) {
	rows := []TargetRow{
		{Ref: "b/history-worst", Generations: 3, LastBest: f64(0.30)},
		{Ref: "c/history-ok", Generations: 1, LastBest: f64(0.80)},
		{Ref: "d/never", NeverEvolved: true},
		{Ref: "a/degraded", NeverEvolved: true, Note: "history unavailable: x"},
	}

	cases := []struct {
		policy, want string
	}{
		{"never-run", "d/never"}, // clean never-run beats degraded despite ref order
		{"", "d/never"},          // empty policy defaults to never-run
		{"worst", "b/history-worst"},
		{"stale", "a/degraded"}, // fewest generations, tie-break by ref
	}
	for _, tc := range cases {
		got, err := SelectTarget(rows, tc.policy)
		if err != nil {
			t.Fatalf("policy %q: %v", tc.policy, err)
		}
		if got != tc.want {
			t.Errorf("policy %q = %s, want %s", tc.policy, got, tc.want)
		}
	}
}

func TestSelectTargetFallbacksAndErrors(t *testing.T) {
	// never-run falls back to worst when everything has history.
	all := []TargetRow{
		{Ref: "x", Generations: 1, LastBest: f64(0.9)},
		{Ref: "y", Generations: 1, LastBest: f64(0.2)},
	}
	if got, _ := SelectTarget(all, SelectNeverRun); got != "y" {
		t.Errorf("never-run fallback = %s, want y (worst)", got)
	}
	// worst falls back to first never-run when nothing has history.
	none := []TargetRow{
		{Ref: "n2", NeverEvolved: true},
		{Ref: "n1", NeverEvolved: true},
	}
	if got, _ := SelectTarget(none, SelectWorst); got != "n1" {
		t.Errorf("worst fallback = %s, want n1", got)
	}
	// Deterministic tie-break by ref.
	tie := []TargetRow{
		{Ref: "z/never", NeverEvolved: true},
		{Ref: "a/never", NeverEvolved: true},
	}
	if got, _ := SelectTarget(tie, SelectNeverRun); got != "a/never" {
		t.Errorf("tie-break = %s, want a/never", got)
	}

	if _, err := SelectTarget(nil, SelectNeverRun); err == nil {
		t.Error("empty rows: want error")
	}
	if _, err := SelectTarget(all, "bogus"); err == nil ||
		!strings.Contains(err.Error(), "unknown selection policy") {
		t.Errorf("bogus policy error = %v", err)
	}
}
