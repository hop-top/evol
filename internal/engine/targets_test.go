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

func TestComputeTrend(t *testing.T) {
	cases := []struct {
		name   string
		scores []float64
		want   *float64
	}{
		{"short", []float64{0.5}, nil},
		{"empty", nil, nil},
		{"two-improving", []float64{0.45, 0.55}, f64(0.10)},
		{"declining", []float64{0.8, 0.7, 0.6, 0.5}, f64(-0.2)}, // recent [0.7 0.6 0.5]=0.6 vs prior [0.8]
		{"flat", []float64{0.5, 0.5, 0.5, 0.5}, f64(0)},
		{"long-decline", []float64{0.9, 0.9, 0.9, 0.6, 0.5, 0.4}, f64(-0.4)}, // recent mean 0.5 vs prior 0.9
	}
	for _, tc := range cases {
		got := computeTrend(tc.scores)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("%s: trend = %v, want nil", tc.name, *got)
		case tc.want != nil && got == nil:
			t.Errorf("%s: trend = nil, want %v", tc.name, *tc.want)
		case tc.want != nil && got != nil:
			if diff := *got - *tc.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("%s: trend = %v, want %v", tc.name, *got, *tc.want)
			}
		}
	}
}

func TestSelectTargetDrift(t *testing.T) {
	rows := []TargetRow{
		{Ref: "a", Generations: 3, Trend: f64(-0.05)},
		{Ref: "b", Generations: 4, Trend: f64(-0.20)},
		{Ref: "c", Generations: 5, Trend: f64(0.10)},
		{Ref: "d", Generations: 1, NeverEvolved: false}, // no trend -> ranks last
	}
	ref, err := SelectTarget(rows, SelectDrift)
	if err != nil || ref != "b" {
		t.Fatalf("drift = %q, %v; want b", ref, err)
	}
	// Tie on trend breaks by ref.
	rows = []TargetRow{
		{Ref: "z", Trend: f64(-0.1)},
		{Ref: "a", Trend: f64(-0.1)},
	}
	if ref, _ = SelectTarget(rows, SelectDrift); ref != "a" {
		t.Fatalf("drift tie = %q, want a", ref)
	}
	// Nothing has a trend -> never-run fallback.
	rows = []TargetRow{
		{Ref: "n", NeverEvolved: true},
		{Ref: "m", Generations: 1},
	}
	if ref, _ = SelectTarget(rows, SelectDrift); ref != "n" {
		t.Fatalf("drift fallback = %q, want n", ref)
	}
}

func TestSelectTargetKBChurn(t *testing.T) {
	rows := []TargetRow{
		{Ref: "evolved-lots", Generations: 9},
		{Ref: "evolved-once", Generations: 1},
		{Ref: "unknown", NeverEvolved: true, Note: "history unavailable: boom"},
		{Ref: "virgin", NeverEvolved: true},
	}
	if ref, err := SelectTarget(rows, SelectKBChurn); err != nil || ref != "virgin" {
		t.Fatalf("kb-churn = %q, %v; want virgin", ref, err)
	}
	// Clean never-run gone: degraded-unknown wins.
	if ref, _ := SelectTarget(rows[:3], SelectKBChurn); ref != "unknown" {
		t.Fatalf("kb-churn degraded = %q, want unknown", ref)
	}
	// Only evolved rows: fewest generations.
	if ref, _ := SelectTarget(rows[:2], SelectKBChurn); ref != "evolved-once" {
		t.Fatalf("kb-churn evolved = %q, want evolved-once", ref)
	}
}

func TestTargetsCarriesScoresAndTrend(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")

	rows, err := New(fakeConfig(t, false)).Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	fake := rows[0]
	if fake.Ref != "skills/fake" {
		t.Fatalf("row order changed: %s", fake.Ref)
	}
	if len(fake.Scores) != 2 || fake.Scores[0] != 0.45 || fake.Scores[1] != 0.55 {
		t.Fatalf("scores = %v, want [0.45 0.55]", fake.Scores)
	}
	if fake.Trend == nil || (*fake.Trend-0.10) > 1e-9 || (*fake.Trend-0.10) < -1e-9 {
		t.Fatalf("trend = %v, want +0.10", fake.Trend)
	}
	if rows[1].Trend != nil || rows[1].Scores != nil {
		t.Fatalf("never-evolved row should carry no scores/trend: %+v", rows[1])
	}
}
