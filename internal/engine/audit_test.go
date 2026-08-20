package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedClock() func() time.Time {
	t0 := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t0 }
}

func readAuditRecords(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl")) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("audit record was never written: %v", err)
	}
	var recs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func auditRunOf(t *testing.T, rec map[string]any) map[string]any {
	t.Helper()
	run, ok := rec["run"].(map[string]any)
	if !ok {
		t.Fatalf("audit record has no run object: %v", rec)
	}
	return run
}

func TestRunEmitsAuditRecordPromoted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	cfg := fakeConfig(t, false)
	cfg.Ports.Audit.Cmd = cfg.Ports.Corpus.Cmd // same fake serves audit
	eng := New(cfg)
	eng.Now = fixedClock()

	res, err := eng.Run(context.Background(), "skills/fake")
	if err != nil || !res.Accepted {
		t.Fatalf("want promotion, got res=%v err=%v", res, err)
	}

	recs := readAuditRecords(t, dir)
	if len(recs) != 1 {
		t.Fatalf("want 1 audit record, got %d", len(recs))
	}
	run := auditRunOf(t, recs[0])
	if run["outcome"] != "promoted" {
		t.Fatalf("outcome: %v", run["outcome"])
	}
	if run["tool"] != "evol" || run["subject"] != "skills/fake" {
		t.Fatalf("identity: %v", run)
	}
	// Deterministic id under the injected clock.
	wantID := runID(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC), "skills/fake")
	if run["run_id"] != wantID {
		t.Fatalf("run_id: got %v want %s", run["run_id"], wantID)
	}
	steps := run["steps"].([]any)
	// baseline + generation-1 (accepted in gen 1).
	if len(steps) != 2 {
		t.Fatalf("steps: %d (%v)", len(steps), steps)
	}
	s0 := steps[0].(map[string]any)
	if s0["name"] != "baseline" || s0["status"] != "ok" {
		t.Fatalf("step 0: %v", s0)
	}
	s1 := steps[1].(map[string]any)
	if s1["name"] != "generation-1" || s1["status"] != "accepted" {
		t.Fatalf("step 1: %v", s1)
	}
	metrics := run["metrics"].(map[string]any)
	if metrics["best_score"].(float64) <= metrics["baseline_score"].(float64) {
		t.Fatalf("metrics: %v", metrics)
	}
}

func TestRunEmitsAuditRecordNoImprovement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	// EVOL_FAKE_GOOD unset -> worse candidate every generation.

	cfg := fakeConfig(t, false)
	cfg.Ports.Audit.Cmd = cfg.Ports.Corpus.Cmd
	eng := New(cfg)
	eng.Now = fixedClock()

	_, err := eng.Run(context.Background(), "skills/fake")
	if !errors.Is(err, ErrNoImprovement) {
		t.Fatalf("want ErrNoImprovement, got %v", err)
	}
	run := auditRunOf(t, readAuditRecords(t, dir)[0])
	if run["outcome"] != "no-improvement" {
		t.Fatalf("outcome: %v", run["outcome"])
	}
	steps := run["steps"].([]any)
	// baseline + 2 explored generations (budget 2).
	if len(steps) != 3 {
		t.Fatalf("steps: %d", len(steps))
	}
	if steps[2].(map[string]any)["status"] != "explored" {
		t.Fatalf("gen step: %v", steps[2])
	}
}

func TestRunEmitsAuditRecordOnGateError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_CASES", "0") // no holdout cases -> ErrGate

	cfg := fakeConfig(t, false)
	cfg.Ports.Audit.Cmd = cfg.Ports.Corpus.Cmd
	eng := New(cfg)
	eng.Now = fixedClock()

	_, err := eng.Run(context.Background(), "skills/fake")
	if !errors.Is(err, ErrGate) {
		t.Fatalf("want ErrGate, got %v", err)
	}
	run := auditRunOf(t, readAuditRecords(t, dir)[0])
	if run["outcome"] != "gate-failed" {
		t.Fatalf("outcome: %v", run["outcome"])
	}
}

func TestAuditUnconfiguredDegradesAndReadsRefuse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")

	cfg := fakeConfig(t, false) // no audit port
	eng := New(cfg)
	var log strings.Builder
	eng.Log = &log

	if _, err := eng.Run(context.Background(), "skills/fake"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(log.String(), "audit: disabled") {
		t.Fatalf("want disabled note, log:\n%s", log.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "audit.jsonl")); err == nil {
		t.Fatal("no audit record should exist")
	}

	if _, err := eng.AuditList(context.Background(), "", 0); !errors.Is(err, ErrAuditUnconfigured) {
		t.Fatalf("list: want ErrAuditUnconfigured, got %v", err)
	}
	if _, err := eng.AuditShow(context.Background(), "x"); !errors.Is(err, ErrAuditUnconfigured) {
		t.Fatalf("show: want ErrAuditUnconfigured, got %v", err)
	}
}

func TestAuditAdapterFailureDegrades(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)
	t.Setenv("EVOL_FAKE_GOOD", "1")
	t.Setenv("EVOL_FAKE_AUDIT", "error")

	cfg := fakeConfig(t, false)
	cfg.Ports.Audit.Cmd = cfg.Ports.Corpus.Cmd
	eng := New(cfg)
	var log strings.Builder
	eng.Log = &log

	res, err := eng.Run(context.Background(), "skills/fake")
	if err != nil || !res.Accepted {
		t.Fatalf("audit failure must not fail the run: res=%v err=%v", res, err)
	}
	if !strings.Contains(log.String(), "audit degraded") {
		t.Fatalf("want degraded warning, log:\n%s", log.String())
	}
}

func TestAuditListAndShowThroughPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOL_TEST_DIR", dir)

	cfg := fakeConfig(t, false)
	cfg.Ports.Audit.Cmd = cfg.Ports.Corpus.Cmd
	eng := New(cfg)

	runs, err := eng.AuditList(context.Background(), "skills/fake", 5)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 1 || runs[0]["run_id"] != "r-newest" {
		t.Fatalf("list rows: %v", runs)
	}

	run, err := eng.AuditShow(context.Background(), "r-newest")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if run["run_id"] != "r-newest" {
		t.Fatalf("show: %v", run)
	}
}
