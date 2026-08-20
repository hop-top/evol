package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Kinds an ArtifactStore serves (spec/port-artifactstore.md).
var artifactKinds = []string{"skill", "prompt", "command", "tool-config"}

// Selection policies for choosing a target when no artifact ref is
// given. Self-scheduling later is just another policy here.
const (
	SelectNeverRun = "never-run"
	SelectWorst    = "worst"
	SelectStale    = "stale"
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
	default:
		return "", fmt.Errorf("unknown selection policy %q (want %s|%s|%s)",
			policy, SelectNeverRun, SelectWorst, SelectStale)
	}
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
