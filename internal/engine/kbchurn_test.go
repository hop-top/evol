package engine

import (
	"context"
	"testing"
	"time"
)

func str(s string) *string { return &s }

// The engine stamps every recorded outcome with its clock; tests inject
// a fixed clock, proving determinism is in the operator's hands.
func TestRecordStampsRecordedAt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	eng := New(fakeConfig(t, false))
	fixed := time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC)
	eng.Now = func() time.Time { return fixed }

	if _, err := eng.Run(context.Background(), "skills/fake"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, rec := range readRecords(t, dir) {
		cands, _ := rec["candidates"].([]any)
		for _, c := range cands {
			got := c.(map[string]any)["recorded_at"]
			if got != "2026-08-20T06:00:00Z" {
				t.Fatalf("recorded_at = %v, want the injected clock", got)
			}
		}
	}
}

func TestTargetsCarriesKBSignals(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")
	t.Setenv("EVOL_FAKE_KB_NEWEST", "2026-08-20T00:00:00Z")

	rows, err := New(fakeConfig(t, true)).Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	fake := rows[0] // skills/fake, 2 generations
	if fake.LastEvolved == nil || *fake.LastEvolved != "2026-08-19T12:00:00Z" {
		t.Fatalf("last_evolved = %v, want the last generation's recorded_at", fake.LastEvolved)
	}
	if fake.KBNewest == nil || *fake.KBNewest != "2026-08-20T00:00:00Z" {
		t.Fatalf("kb_newest = %v, want the adapter's ts", fake.KBNewest)
	}
	if rows[1].LastEvolved != nil {
		t.Fatalf("never-evolved row must carry no last_evolved: %+v", rows[1])
	}
}

// An adapter without the optional `newest` action (exit non-zero)
// degrades to no signal — targets still succeed.
func TestTargetsKBNewestDegrades(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")
	// EVOL_FAKE_KB_NEWEST unset -> fake adapter faults on `newest`.

	rows, err := New(fakeConfig(t, true)).Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets must not fail on newest degradation: %v", err)
	}
	for _, r := range rows {
		if r.KBNewest != nil {
			t.Fatalf("kb_newest should be nil under degradation: %+v", r)
		}
	}
}

// Null ts (knowledge exists but nothing is timestamped) is a valid
// no-signal answer, distinct from action failure.
func TestTargetsKBNewestNullTS(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")
	t.Setenv("EVOL_FAKE_KB_NEWEST", "none")

	rows, err := New(fakeConfig(t, true)).Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if rows[0].KBNewest != nil {
		t.Fatalf("null ts must yield no signal, got %v", *rows[0].KBNewest)
	}
}

func TestSelectKBChurnLadder(t *testing.T) {
	churnOld := TargetRow{Ref: "b/churn-old", Generations: 2,
		KBNewest: str("2026-08-19T00:00:00Z"), LastEvolved: str("2026-08-18T00:00:00Z")}
	churnNew := TargetRow{Ref: "c/churn-new", Generations: 5,
		KBNewest: str("2026-08-20T00:00:00Z"), LastEvolved: str("2026-08-18T00:00:00Z")}
	quiet := TargetRow{Ref: "a/quiet", Generations: 1,
		KBNewest: str("2026-08-01T00:00:00Z"), LastEvolved: str("2026-08-18T00:00:00Z")}
	never := TargetRow{Ref: "d/never", NeverEvolved: true}
	degraded := TargetRow{Ref: "e/degraded", NeverEvolved: true, Note: "history unavailable: x"}

	// Rung 1: churn beats everything, most-recent knowledge first —
	// generation counts and ref order do not override the signal.
	if ref, err := SelectTarget([]TargetRow{quiet, churnOld, churnNew, never, degraded}, SelectKBChurn); err != nil || ref != "c/churn-new" {
		t.Fatalf("churn pick = %q, %v; want c/churn-new", ref, err)
	}
	// Equal kb_newest ties break by ref.
	tie := churnOld
	tie.Ref = "a/churn-tie"
	tie.KBNewest = churnNew.KBNewest
	if ref, _ := SelectTarget([]TargetRow{churnNew, tie}, SelectKBChurn); ref != "a/churn-tie" {
		t.Fatalf("tie = %q, want a/churn-tie (ref order)", ref)
	}
	// Rung 2: no measurable churn -> clean never-run.
	if ref, _ := SelectTarget([]TargetRow{quiet, never, degraded}, SelectKBChurn); ref != "d/never" {
		t.Fatalf("rung 2 = %q, want d/never", ref)
	}
	// Rung 3: degraded-unknown.
	if ref, _ := SelectTarget([]TargetRow{quiet, degraded}, SelectKBChurn); ref != "e/degraded" {
		t.Fatalf("rung 3 = %q, want e/degraded", ref)
	}
	// Rung 4: fewest generations, when no row shows churn and none is
	// never-run.
	quietMany := TargetRow{Ref: "b/quiet-many", Generations: 7,
		KBNewest: str("2026-08-01T00:00:00Z"), LastEvolved: str("2026-08-18T00:00:00Z")}
	if ref, _ := SelectTarget([]TargetRow{quietMany, quiet}, SelectKBChurn); ref != "a/quiet" {
		t.Fatalf("rung 4 = %q, want a/quiet (fewest generations)", ref)
	}
	// Determinism: same inputs, same answer, ten times.
	rows := []TargetRow{quiet, churnOld, churnNew, never, degraded}
	for i := 0; i < 10; i++ {
		if ref, _ := SelectTarget(rows, SelectKBChurn); ref != "c/churn-new" {
			t.Fatalf("run %d nondeterministic: %q", i, ref)
		}
	}
}
