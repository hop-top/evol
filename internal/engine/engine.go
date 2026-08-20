// Package engine runs the evol self-improvement loop: load an
// artifact, evaluate a baseline, propose candidate revisions, execute
// and score them against eval cases, gate against the baseline, and
// promote or reject — writing every verdict back to the corpus.
//
// The engine owns control flow only; every I/O exchange crosses a port
// (see spec/). Scoring uses an engine-level draft contract documented
// in docs/scorer-draft.md until a Scorer port is extracted.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hop.top/evol/internal/port"
)

// ErrNoImprovement reports a fully-run loop where no candidate beat
// the gate (CLI exit class 1).
var ErrNoImprovement = errors.New("no candidate improved on the baseline")

// ErrGate reports unmet preconditions — no eval cases, empty artifact
// (CLI exit class 2).
var ErrGate = errors.New("gate precondition failed")

// Engine wires the port clients and executes runs.
type Engine struct {
	cfg Config

	store     *port.Client
	generator *port.Client
	executor  *port.Client
	corpus    *port.Client
	scorer    *port.Client
	kb        *port.Client
	casegen   *port.Client

	// Log receives progress lines; defaults to io.Discard.
	Log io.Writer

	// Now supplies wall-clock time for corpus record stamps; defaults to
	// time.Now. Tests inject a fixed clock for determinism.
	Now func() time.Time

	// sigWarned dedups the small-sample significance warning per run.
	sigWarned bool
}

// New builds an Engine from a normalized Config.
func New(cfg Config) *Engine {
	return &Engine{
		cfg:       cfg,
		store:     cfg.Ports.ArtifactStore.client("artifactstore"),
		generator: cfg.Ports.Generator.client("generator"),
		executor:  cfg.Ports.Executor.client("executor"),
		corpus:    cfg.Ports.Corpus.client("corpus"),
		scorer:    cfg.Ports.Scorer.client("scorer"),
		kb:        cfg.Ports.KnowledgeBase.client("knowledgebase"),
		casegen:   cfg.Ports.CaseGen.client("generator"),
		Log:       io.Discard,
		Now:       time.Now,
	}
}

func (e *Engine) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(e.Log, format+"\n", args...)
}

// Run executes the loop for one artifact ref.
func (e *Engine) Run(ctx context.Context, artifactRef string) (*Result, error) {
	// 1. Load the artifact.
	var loadResp struct {
		Artifact Artifact `json:"artifact"`
	}
	if err := e.store.Call(ctx, "load",
		map[string]any{"ref": artifactRef}, &loadResp); err != nil {
		return nil, err
	}
	artifact := loadResp.Artifact
	if strings.TrimSpace(artifact.Body) == "" {
		return nil, fmt.Errorf("%w: artifact %s has an empty body", ErrGate, artifactRef)
	}

	// 2. Eval cases for the gating split.
	var casesResp struct {
		Cases []Case `json:"cases"`
	}
	if err := e.corpus.Call(ctx, "cases", map[string]any{
		"artifact_ref": artifactRef,
		"split":        e.cfg.Holdout,
	}, &casesResp); err != nil {
		return nil, err
	}
	cases := casesResp.Cases

	// Human corrections are promoted into the gating pool. Failures
	// degrade — older corpus adapters may not serve the action.
	if corr := e.corrections(ctx, artifactRef); len(corr) > 0 {
		before := len(cases)
		cases = mergeCases(cases, corr, e.cfg.Holdout)
		e.logf("corrections: merged %d of %d into the %q pool",
			len(cases)-before, len(corr), e.cfg.Holdout)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%w: no %q cases for %s", ErrGate, e.cfg.Holdout, artifactRef)
	}

	staging, err := os.MkdirTemp("", "evol-staging-*")
	if err != nil {
		return nil, fmt.Errorf("staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	// 3. Baseline: current artifact over all cases, under the primary
	// provider. Secondary providers (model-dimension sweep) are scored
	// once and recorded as evidence — never gated on.
	primary := e.cfg.primaryProvider()
	baselineScores, err := e.evaluate(ctx, staging, "baseline", artifact.Frontmatter, artifact.Body, primary, cases)
	if err != nil {
		return nil, err
	}
	baselineMean := mean(baselineScores)
	e.logf("baseline %s@%s: %.4f over %d case(s) × %d trial(s)",
		artifact.Ref, artifact.Version, baselineMean, len(cases), e.cfg.Thresholds.Trials)

	if evidence, err := e.sweep(ctx, staging, "baseline", artifact.Frontmatter, artifact.Body, "", cases); err != nil {
		return nil, err
	} else if len(evidence) > 0 {
		// Generation 0 holds baseline sweep evidence.
		if err := e.record(ctx, artifact, 0, evidence); err != nil {
			return nil, err
		}
	}

	result := &Result{
		ArtifactRef:     artifact.Ref,
		BaselineVersion: artifact.Version,
		BaselineScore:   baselineMean,
		BestScore:       baselineMean,
	}
	history := []ScoreSummary{{Version: artifact.Version, Score: baselineMean}}

	// 4. Generations.
	for gen := 1; gen <= e.cfg.Budget.Generations; gen++ {
		result.Generations = gen

		tabu, err := e.tabu(ctx, artifactRef)
		if err != nil {
			return nil, err
		}
		knowledge := e.knowledge(ctx, artifactRef)

		candidates, err := e.propose(ctx, artifact, history, tabu, knowledge)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			e.logf("generation %d: dry (no candidates proposed)", gen)
			if err := e.record(ctx, artifact, gen, nil); err != nil {
				return nil, err
			}
			continue
		}

		outcomes := make([]CandidateOutcome, 0, len(candidates))
		var accepted *Candidate
		var acceptedMean float64

		for i := range candidates {
			cand := candidates[i]
			result.CandidatesTried++

			scores, evalErr := e.evaluate(ctx, staging, cand.ID, cand.Frontmatter, cand.Body, primary, cases)
			if evalErr != nil {
				return nil, evalErr
			}
			candMean := mean(scores)
			if candMean > result.BestScore {
				result.BestScore = candMean
			}

			outcome := CandidateOutcome{ID: cand.ID, Scores: scores, Strategy: cand.Strategy, Provider: primary}
			switch {
			case allFailed(scores):
				outcome.Verdict = VerdictFailed
				outcome.Rationale = "every case run failed"
			case candMean >= baselineMean+e.cfg.Thresholds.Delta:
				sig := e.significance(baselineScores, scores)
				if sig.tested && sig.p > e.cfg.Thresholds.SigLevel {
					outcome.Verdict = VerdictRejected
					outcome.Rationale = fmt.Sprintf(
						"holdout mean %.4f cleared baseline %.4f + delta %.4f but the improvement is not significant (paired bootstrap p=%.4f > %.4f)",
						candMean, baselineMean, e.cfg.Thresholds.Delta, sig.p, e.cfg.Thresholds.SigLevel)
					break
				}
				outcome.Verdict = VerdictAccepted
				outcome.Rationale = fmt.Sprintf(
					"holdout mean %.4f ≥ baseline %.4f + delta %.4f",
					candMean, baselineMean, e.cfg.Thresholds.Delta)
				if sig.tested {
					outcome.Rationale += fmt.Sprintf(
						"; paired bootstrap p=%.4f ≤ %.4f", sig.p, e.cfg.Thresholds.SigLevel)
				}
				if accepted == nil {
					accepted = &candidates[i]
					acceptedMean = candMean
					if sig.tested {
						p := sig.p
						result.SigP = &p
					}
					// Pin the recorded environment for the promoted run
					// so it can serve as a regression fixture.
					if e.cfg.FixturesDir != "" {
						outcome.Fixtures = &Fixtures{CassetteDir: e.cfg.FixturesDir}
					}
				}
			default:
				outcome.Verdict = VerdictRejected
				outcome.Rationale = fmt.Sprintf(
					"holdout mean %.4f below baseline %.4f + delta %.4f",
					candMean, baselineMean, e.cfg.Thresholds.Delta)
			}
			e.logf("generation %d: candidate %s (%s) mean %.4f → %s",
				gen, cand.ID, cand.Strategy, candMean, outcome.Verdict)
			outcomes = append(outcomes, outcome)
			evidence, evErr := e.sweep(ctx, staging, cand.ID, cand.Frontmatter, cand.Body, cand.Strategy, cases)
			if evErr != nil {
				return nil, evErr
			}
			outcomes = append(outcomes, evidence...)
			history = append(history, ScoreSummary{
				Version: cand.ID, Score: candMean, Feedback: outcome.Rationale,
			})
		}

		// Write-back is mandatory: every candidate, every verdict.
		if err := e.record(ctx, artifact, gen, outcomes); err != nil {
			return nil, err
		}

		if accepted != nil {
			var writeResp struct {
				Version string `json:"version"`
				// GitCommit is set by git-native artifact stores; empty
				// otherwise. Surfaced for post-promotion hooks and audit.
				GitCommit string `json:"git_commit"`
			}
			msg := fmt.Sprintf("evolve %s: %s — %s",
				artifact.Ref, accepted.Strategy, accepted.Rationale)
			if err := e.store.Call(ctx, "write", map[string]any{
				"ref":         artifact.Ref,
				"frontmatter": accepted.Frontmatter,
				"body":        accepted.Body,
				"message":     msg,
			}, &writeResp); err != nil {
				return nil, err
			}
			result.Accepted = true
			result.AcceptedID = accepted.ID
			result.NewVersion = writeResp.Version
			result.GitCommit = writeResp.GitCommit
			result.BestScore = acceptedMean
			e.logf("accepted %s as %s@%s", accepted.ID, artifact.Ref, writeResp.Version)
			return result, nil
		}
	}

	return result, ErrNoImprovement
}

// evaluate stages a document, runs every case × trials through the
// Executor, and scores each transcript. Run-level failures score 0.0
// and carry the error as the reason — failure is data.
func (e *Engine) evaluate(ctx context.Context, staging, id, frontmatter, body, provider string, cases []Case) ([]CaseScore, error) {
	ref, err := stage(staging, id, frontmatter, body)
	if err != nil {
		return nil, err
	}

	scores := make([]CaseScore, 0, len(cases)*e.cfg.Thresholds.Trials)
	for _, cs := range cases {
		for trial := 0; trial < e.cfg.Thresholds.Trials; trial++ {
			var runResp struct {
				Transcript Transcript `json:"transcript"`
				Error      string     `json:"error"`
			}
			execEnv := map[string]any{"mode": e.cfg.ExecutorMode}
			if provider != "" {
				execEnv["provider"] = provider
			}
			if err := e.executor.Call(ctx, "run", map[string]any{
				"candidate_ref": ref,
				"case":          map[string]any{"id": cs.ID, "input": cs.Input},
				"env":           execEnv,
			}, &runResp); err != nil {
				return nil, err
			}
			if runResp.Error != "" {
				scores = append(scores, CaseScore{
					CaseID: cs.ID, Score: 0,
					Reason: "run failed: " + runResp.Error,
				})
				continue
			}

			var scoreResp struct {
				Score struct {
					Value  float64 `json:"value"`
					Reason string  `json:"reason"`
				} `json:"score"`
			}
			if err := e.scorer.Call(ctx, "score", map[string]any{
				"case":       map[string]any{"id": cs.ID, "input": cs.Input, "expected": cs.Expected},
				"transcript": runResp.Transcript,
			}, &scoreResp); err != nil {
				return nil, err
			}
			scores = append(scores, CaseScore{
				CaseID: cs.ID,
				Score:  scoreResp.Score.Value,
				Reason: scoreResp.Score.Reason,
			})
		}
	}
	return scores, nil
}

// sweep scores one document under every secondary provider and shapes
// the results as evidence outcomes (model-dimension sweep). Evidence is
// recorded for routing decisions downstream; it never gates.
func (e *Engine) sweep(ctx context.Context, staging, id, frontmatter, body, strategy string, cases []Case) ([]CandidateOutcome, error) {
	secondaries := e.cfg.secondaryProviders()
	if len(secondaries) == 0 {
		return nil, nil
	}
	outcomes := make([]CandidateOutcome, 0, len(secondaries))
	for _, provider := range secondaries {
		scores, err := e.evaluate(ctx, staging, id, frontmatter, body, provider, cases)
		if err != nil {
			return nil, err
		}
		e.logf("sweep %s under %s: %.4f (evidence)", id, provider, mean(scores))
		outcomes = append(outcomes, CandidateOutcome{
			ID:        id,
			Scores:    scores,
			Strategy:  strategy,
			Verdict:   VerdictEvidence,
			Rationale: "provider sweep evidence; not gated",
			Provider:  provider,
		})
	}
	return outcomes, nil
}

// sigResult carries one significance decision.
type sigResult struct {
	tested bool
	p      float64
}

// significance runs the paired bootstrap when enough paired cases
// exist; below the floor it degrades to mean-only gating with a logged
// warning (once per run).
func (e *Engine) significance(baseline, candidate []CaseScore) sigResult {
	diffs := pairedDiffs(baseline, candidate)
	if len(diffs) < sigMinPairs {
		if !e.sigWarned {
			e.sigWarned = true
			e.logf("significance disabled: %d paired case(s) < %d floor; gating on mean only",
				len(diffs), sigMinPairs)
		}
		return sigResult{}
	}
	return sigResult{
		tested: true,
		p:      bootstrapP(diffs, sigResamples, e.cfg.Thresholds.SigSeed),
	}
}

// corrections fetches human-corrected cases from the Corpus. Any error
// degrades to none — the action is newer than some adapters.
func (e *Engine) corrections(ctx context.Context, artifactRef string) []Case {
	var resp struct {
		Cases []Case `json:"cases"`
	}
	if err := e.corpus.Call(ctx, "corrections",
		map[string]any{"artifact_ref": artifactRef}, &resp); err != nil {
		e.logf("corrections degraded: %v", err)
		return nil
	}
	return resp.Cases
}

// mergeCases appends extras into base, deduping by case id and keeping
// only entries whose split matches the gating split (empty split
// entries are taken as-is).
func mergeCases(base, extra []Case, split string) []Case {
	seen := make(map[string]bool, len(base))
	for _, c := range base {
		seen[c.ID] = true
	}
	out := base
	for _, c := range extra {
		if c.Split != "" && c.Split != split {
			continue
		}
		if c.ID == "" || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	return out
}

func (e *Engine) tabu(ctx context.Context, artifactRef string) ([]TabuEntry, error) {
	var resp struct {
		Entries []TabuEntry `json:"entries"`
	}
	if err := e.corpus.Call(ctx, "tabu",
		map[string]any{"artifact_ref": artifactRef}, &resp); err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

// knowledge queries the optional KnowledgeBase port. Any failure or
// unavailability degrades to nil — proposals proceed without it.
func (e *Engine) knowledge(ctx context.Context, artifactRef string) []Passage {
	if !e.kb.Configured() {
		return nil
	}
	var resp struct {
		Unavailable bool      `json:"unavailable"`
		Passages    []Passage `json:"passages"`
	}
	if err := e.kb.Call(ctx, "search", map[string]any{
		"query": artifactRef, "limit": 5,
	}, &resp); err != nil {
		e.logf("knowledgebase degraded: %v", err)
		return nil
	}
	if resp.Unavailable {
		e.logf("knowledgebase unavailable; continuing without knowledge")
		return nil
	}
	return resp.Passages
}

func (e *Engine) propose(ctx context.Context, artifact Artifact, history []ScoreSummary, tabu []TabuEntry, knowledge []Passage) ([]Candidate, error) {
	params := map[string]any{
		"artifact": artifact,
		"scores":   history,
		"tabu":     emptyNotNull(tabu),
		"budget":   map[string]any{"max_candidates": e.cfg.Budget.MaxCandidates},
	}
	if knowledge != nil {
		params["knowledge"] = knowledge
	}
	var resp struct {
		Candidates []Candidate `json:"candidates"`
	}
	if err := e.generator.Call(ctx, "propose", params, &resp); err != nil {
		return nil, err
	}
	if len(resp.Candidates) > e.cfg.Budget.MaxCandidates {
		resp.Candidates = resp.Candidates[:e.cfg.Budget.MaxCandidates]
	}
	return resp.Candidates, nil
}

func (e *Engine) record(ctx context.Context, artifact Artifact, generation int, outcomes []CandidateOutcome) error {
	stamp := e.Now().UTC().Format(time.RFC3339)
	for i := range outcomes {
		if outcomes[i].RecordedAt == "" {
			outcomes[i].RecordedAt = stamp
		}
	}
	return e.corpus.Call(ctx, "record", map[string]any{
		"generation": map[string]any{
			"artifact_ref":     artifact.Ref,
			"baseline_version": artifact.Version,
			"number":           generation,
		},
		"candidates": emptyNotNull(outcomes),
	}, nil)
}

// stage writes a complete artifact document (frontmatter + body) into
// the staging dir and returns its path as the candidate_ref.
func stage(dir, id, frontmatter, body string) (string, error) {
	doc := body
	if strings.TrimSpace(frontmatter) != "" {
		doc = "---\n" + strings.TrimRight(frontmatter, "\n") + "\n---\n\n" + body
	}
	path := filepath.Join(dir, sanitize(id)+".md")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		return "", fmt.Errorf("stage %s: %w", id, err)
	}
	return path, nil
}

func sanitize(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, id)
}

func mean(scores []CaseScore) float64 {
	if len(scores) == 0 {
		return 0
	}
	var sum float64
	for _, s := range scores {
		sum += s.Score
	}
	return sum / float64(len(scores))
}

func allFailed(scores []CaseScore) bool {
	for _, s := range scores {
		if !strings.HasPrefix(s.Reason, "run failed:") {
			return false
		}
	}
	return len(scores) > 0
}

// emptyNotNull keeps empty slices as [] on the wire rather than null.
func emptyNotNull[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
