package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Kinds an ArtifactStore serves (spec/port-artifactstore.md).
var artifactKinds = []string{"skill", "prompt", "command", "tool-config"}

// Selection policies for choosing a target when no artifact ref is
// given. Self-scheduling is these policies on a schedule: an operator
// (or cron) runs `evol run --select drift` and the loop picks its own
// next target from recorded evidence.
const (
	SelectNeverRun = "never-run"
	SelectWorst    = "worst"
	SelectStale    = "stale"
	// SelectDrift targets the artifact whose recent generations trend
	// most negative (score decline). Artifacts with fewer than two
	// generations carry no trend and rank last.
	SelectDrift = "drift"
	// SelectKBChurn targets artifacts whose knowledge moved after their
	// last evolution: the KnowledgeBase optional `newest` action supplies
	// a per-ref timestamp, compared against the last generation's
	// recorded_at. When the signal is absent (KB unconfigured, action
	// unsupported, no timestamps) the policy degrades down a documented
	// ladder — churn rows, then clean never-run, then degraded-unknown,
	// then fewest generations. See docs/self-scheduling.md.
	SelectKBChurn = "kb-churn"
)

// ErrNoTargets reports an artifact store with nothing to evolve.
var ErrNoTargets = errors.New("no artifacts available to target")

// TargetRow is one artifact's evolution status, as reported by
// `evol targets` and consumed by selection policies.
type TargetRow struct {
	Ref          string   `json:"ref"`
	Kind         string   `json:"kind"`
	Generations  int      `json:"generations"`
	LastBest     *float64 `json:"last_best_score,omitempty"`
	LastVerdict  string   `json:"last_verdict,omitempty"`
	NeverEvolved bool     `json:"never_evolved"`
	// Scores are the per-generation best scores, generation order.
	Scores []float64 `json:"scores,omitempty"`
	// Trend is mean(recent generations) - mean(prior): negative =
	// declining. Computed when at least two generations exist; the
	// recent window is the last min(3, n-1) generations.
	Trend *float64 `json:"trend,omitempty"`
	// KBNewest is the newest knowledge timestamp matching this ref
	// (RFC3339), from the KnowledgeBase optional `newest` action; nil
	// when the KB is unconfigured, the action is unsupported, or nothing
	// matching carries a timestamp.
	KBNewest *string `json:"kb_newest,omitempty"`
	// LastEvolved is the recorded_at of the artifact's last generation
	// (RFC3339); nil for never-evolved artifacts and rows recorded before
	// timestamps existed.
	LastEvolved *string `json:"last_evolved,omitempty"`
	// Note carries a degradation reason when history was unavailable
	// (older corpus adapters); such rows count as never-evolved for
	// selection but rank after clean never-run rows.
	Note string `json:"note,omitempty"`
}

// historyGeneration mirrors the Corpus `history` response entries.
type historyGeneration struct {
	Generation int     `json:"generation"`
	BestScore  float64 `json:"best_score"`
	Verdict    string  `json:"verdict"`
	RecordedAt string  `json:"recorded_at,omitempty"`
}

// Targets enumerates every artifact the store serves, joined with its
// corpus history. Per-row history failures degrade to unknowns with a
// note — the corpus `history` action is newer than some adapters.
func (e *Engine) Targets(ctx context.Context) ([]TargetRow, error) {
	rows := make([]TargetRow, 0)
	for _, kind := range artifactKinds {
		var listResp struct {
			Refs []string `json:"refs"`
		}
		if err := e.store.Call(ctx, "list",
			map[string]any{"kind": kind}, &listResp); err != nil {
			return nil, fmt.Errorf("artifactstore list %s: %w", kind, err)
		}
		for _, ref := range listResp.Refs {
			row := TargetRow{Ref: ref, Kind: kind}
			var histResp struct {
				Generations []historyGeneration `json:"generations"`
			}
			if err := e.corpus.Call(ctx, "history",
				map[string]any{"artifact_ref": ref}, &histResp); err != nil {
				row.NeverEvolved = true
				row.Note = "history unavailable: " + err.Error()
				e.logf("targets: history degraded for %s: %v", ref, err)
			} else if len(histResp.Generations) == 0 {
				row.NeverEvolved = true
			} else {
				row.Generations = len(histResp.Generations)
				last := histResp.Generations[len(histResp.Generations)-1]
				score := last.BestScore
				row.LastBest = &score
				row.LastVerdict = last.Verdict
				row.Scores = make([]float64, 0, len(histResp.Generations))
				for _, g := range histResp.Generations {
					row.Scores = append(row.Scores, g.BestScore)
				}
				row.Trend = computeTrend(row.Scores)
				if ts := last.RecordedAt; ts != "" {
					row.LastEvolved = &ts
				}
			}
			if ts := e.kbNewest(ctx, ref); ts != nil {
				row.KBNewest = ts
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ref < rows[j].Ref })
	return rows, nil
}

// SelectTarget picks one ref by policy. Ties break by ref, so a given
// corpus state always selects the same target.
//
//   - never-run: first clean never-evolved row; then history-unknown
//     rows; falls back to worst when everything has history.
//   - worst: lowest last best score among rows with history; falls back
//     to never-run when nothing has history.
//   - stale: fewest recorded generations (generations are the staleness
//     proxy — the corpus stores no wall-clock by design).
//   - drift: most negative score trend across recent generations;
//     trendless rows (<2 generations) rank last; falls back to
//     never-run when nothing carries a trend.
//   - kb-churn: knowledge newer than the last evolution (KB `newest`
//     vs last generation recorded_at), most-recent knowledge first;
//     degrades rung by rung to the attention-starvation ladder —
//     never-evolved first, then fewest generations (see
//     docs/self-scheduling.md).
func SelectTarget(rows []TargetRow, policy string) (string, error) {
	if len(rows) == 0 {
		return "", ErrNoTargets
	}
	sorted := make([]TargetRow, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ref < sorted[j].Ref })

	switch policy {
	case SelectNeverRun, "":
		for _, r := range sorted {
			if r.NeverEvolved && r.Note == "" {
				return r.Ref, nil
			}
		}
		for _, r := range sorted {
			if r.NeverEvolved {
				return r.Ref, nil
			}
		}
		return selectWorst(sorted)
	case SelectWorst:
		if ref, err := selectWorst(sorted); err == nil {
			return ref, nil
		}
		// Nothing has history yet; any never-run row is the worst known.
		return sorted[0].Ref, nil
	case SelectStale:
		best := sorted[0]
		for _, r := range sorted[1:] {
			if r.Generations < best.Generations {
				best = r
			}
		}
		return best.Ref, nil
	case SelectDrift:
		var pick *TargetRow
		for i := range sorted {
			r := &sorted[i]
			if r.Trend == nil {
				continue
			}
			if pick == nil || *r.Trend < *pick.Trend {
				pick = r
			}
		}
		if pick != nil {
			return pick.Ref, nil
		}
		// No artifact carries a trend yet (fewer than two generations
		// everywhere): fall back to never-run so the loop still moves.
		return SelectTarget(sorted, SelectNeverRun)
	case SelectKBChurn:
		// Rung 1: measurable churn — knowledge newer than the last
		// evolution. Most-recent knowledge first; ties break by ref (the
		// stable sort above already ordered by ref).
		var churn *TargetRow
		for i := range sorted {
			r := &sorted[i]
			kb, evolved := parseRFC3339(r.KBNewest), parseRFC3339(r.LastEvolved)
			if kb == nil || evolved == nil || !kb.After(*evolved) {
				continue
			}
			if churn == nil {
				churn = r
				continue
			}
			ckb := parseRFC3339(churn.KBNewest)
			if kb.After(*ckb) {
				churn = r
			}
		}
		if churn != nil {
			return churn.Ref, nil
		}
		// Rungs 2-4: the v0 attention-starvation ladder.
		for _, r := range sorted {
			if r.NeverEvolved && r.Note == "" {
				return r.Ref, nil
			}
		}
		for _, r := range sorted {
			if r.NeverEvolved {
				return r.Ref, nil
			}
		}
		best := sorted[0]
		for _, r := range sorted[1:] {
			if r.Generations < best.Generations {
				best = r
			}
		}
		return best.Ref, nil
	default:
		return "", fmt.Errorf("unknown selection policy %q (want %s|%s|%s|%s|%s)",
			policy, SelectNeverRun, SelectWorst, SelectStale, SelectDrift, SelectKBChurn)
	}
}

// computeTrend returns mean(recent) - mean(prior) over per-generation
// best scores, where recent is the last min(3, n-1) entries. Nil when
// fewer than two generations exist. Negative values mean decline.
func computeTrend(scores []float64) *float64 {
	n := len(scores)
	if n < 2 {
		return nil
	}
	recentN := n - 1
	if recentN > 3 {
		recentN = 3
	}
	mean := func(xs []float64) float64 {
		var s float64
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs))
	}
	t := mean(scores[n-recentN:]) - mean(scores[:n-recentN])
	return &t
}

func selectWorst(sorted []TargetRow) (string, error) {
	var pick *TargetRow
	for i := range sorted {
		r := &sorted[i]
		if r.LastBest == nil {
			continue
		}
		if pick == nil || *r.LastBest < *pick.LastBest {
			pick = r
		}
	}
	if pick == nil {
		return "", ErrNoTargets
	}
	return pick.Ref, nil
}

// kbNewest asks the optional KnowledgeBase `newest` action for the
// newest knowledge timestamp matching a ref. Every failure mode —
// unconfigured port, unsupported action, unavailability, absent or
// unparseable timestamp — degrades to nil; churn selection then falls
// down its ladder rather than guessing.
func (e *Engine) kbNewest(ctx context.Context, ref string) *string {
	if !e.kb.Configured() {
		return nil
	}
	var resp struct {
		Unavailable bool    `json:"unavailable"`
		TS          *string `json:"ts"`
	}
	if err := e.kb.Call(ctx, "newest", map[string]any{"query": ref}, &resp); err != nil {
		e.logf("kb newest degraded for %s: %v", ref, err)
		return nil
	}
	if resp.Unavailable || resp.TS == nil || parseRFC3339(resp.TS) == nil {
		return nil
	}
	return resp.TS
}

// parseRFC3339 parses an optional RFC3339 string; nil in, nil out, and
// unparseable values degrade to nil rather than erroring.
func parseRFC3339(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}
