package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrAuditUnconfigured reports a read on the run ledger with no
// ports.audit configured (CLI exit class 3, with the adapter hint).
var ErrAuditUnconfigured = errors.New(
	"no ports.audit configured — bind an adapter (audit-tlc for the family tracker, audit-fs for a plain JSONL ledger)")

// AuditStep is one phase of a run, as recorded to the Audit port.
type AuditStep struct {
	Seq    int    `json:"seq"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// AuditRun is the run record shape of spec/port-audit.md.
type AuditRun struct {
	Tool       string         `json:"tool"`
	RunID      string         `json:"run_id"`
	Subject    string         `json:"subject"`
	StartedAt  string         `json:"started_at"`
	FinishedAt string         `json:"finished_at"`
	Outcome    string         `json:"outcome"`
	Metrics    map[string]any `json:"metrics,omitempty"`
	Steps      []AuditStep    `json:"steps,omitempty"`
}

func (e *Engine) auditConfigured() bool {
	return len(e.cfg.Ports.Audit.Cmd) > 0
}

// addStep appends to the current run's audit trail; seq is positional.
func (e *Engine) addStep(name, status, detail string) {
	e.steps = append(e.steps, AuditStep{
		Seq: len(e.steps), Name: name, Status: status, Detail: detail,
	})
}

// runID derives a stable id from the start instant and the subject —
// deterministic under an injected clock, unique enough under the real
// one (one loop run per subject per second).
func runID(started time.Time, ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return fmt.Sprintf("run-%s-%s",
		started.Format("20060102T150405Z"), hex.EncodeToString(sum[:])[:8])
}

// outcomeOf maps the run result to the specced outcome strings.
func outcomeOf(res *Result, err error) string {
	switch {
	case err == nil:
		return "promoted"
	case errors.Is(err, ErrNoImprovement):
		return "no-improvement"
	case errors.Is(err, ErrGate):
		return "gate-failed"
	default:
		return "error"
	}
}

// emitAudit records the finished run. Audit must never fail a run:
// unconfigured → one note; adapter failure → one warning. Both degrade.
func (e *Engine) emitAudit(ctx context.Context, ref string, started time.Time, res *Result, runErr error) {
	if !e.auditConfigured() {
		e.logf("audit: disabled (no ports.audit configured)")
		return
	}

	rec := AuditRun{
		Tool:       "evol",
		RunID:      runID(started, ref),
		Subject:    ref,
		StartedAt:  started.Format(time.RFC3339),
		FinishedAt: e.Now().UTC().Format(time.RFC3339),
		Outcome:    outcomeOf(res, runErr),
		Steps:      e.steps,
	}
	if res != nil {
		rec.Metrics = map[string]any{
			"baseline_score":   res.BaselineScore,
			"best_score":       res.BestScore,
			"generations":      res.Generations,
			"candidates_tried": res.CandidatesTried,
		}
		if res.SigP != nil {
			rec.Metrics["sig_p"] = *res.SigP
		}
	}

	var resp struct{}
	if err := e.audit.Call(ctx, "record", map[string]any{"run": rec}, &resp); err != nil {
		e.logf("audit degraded: %v", err)
		return
	}
	e.logf("audit: recorded %s (%s)", rec.RunID, rec.Outcome)
}

// AuditList reads the ledger through the Audit port, newest first.
// Rows are raw maps so tracker-side extra fields survive display.
func (e *Engine) AuditList(ctx context.Context, subject string, limit int) ([]map[string]any, error) {
	if !e.auditConfigured() {
		return nil, ErrAuditUnconfigured
	}
	req := map[string]any{"tool": "evol"}
	if subject != "" {
		req["subject"] = subject
	}
	if limit > 0 {
		req["limit"] = limit
	}
	var resp struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := e.audit.Call(ctx, "list", req, &resp); err != nil {
		return nil, err
	}
	return resp.Runs, nil
}

// AuditShow reads one full run record through the Audit port.
func (e *Engine) AuditShow(ctx context.Context, id string) (map[string]any, error) {
	if !e.auditConfigured() {
		return nil, ErrAuditUnconfigured
	}
	var resp struct {
		Run map[string]any `json:"run"`
	}
	if err := e.audit.Call(ctx, "show", map[string]any{
		"tool": "evol", "run_id": id,
	}, &resp); err != nil {
		return nil, err
	}
	if resp.Run == nil {
		return nil, fmt.Errorf("audit: run %q not found", id)
	}
	return resp.Run, nil
}
