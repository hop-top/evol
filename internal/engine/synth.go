package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrNoCaseGen reports a synthesis request without a configured casegen
// port (CLI exit class 3).
var ErrNoCaseGen = errors.New("ports.casegen.cmd is not configured")

// ErrNoKnowledge reports a synthesis attempt with no grounding
// knowledge available: cases invented from the artifact text alone are
// circular evals, so the engine refuses (CLI exit class 2).
var ErrNoKnowledge = errors.New("no knowledge passages available; grounded synthesis requires a knowledgebase")

// maxSynthExamples bounds how many existing cases travel to the casegen
// adapter as style references.
const maxSynthExamples = 5

// SynthCase is one generated eval case preview returned to the caller.
type SynthCase struct {
	ID        string `json:"id"`
	Input     string `json:"input"`
	Expected  string `json:"expected,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}

// SynthResult is the outcome of one synthesis run. All added cases are
// quarantined: they join the eval pool only after review + promotion.
type SynthResult struct {
	ArtifactRef string      `json:"artifact_ref"`
	Generated   int         `json:"generated"`
	Added       int         `json:"added"`
	Duplicates  int         `json:"duplicates"`
	Quarantined bool        `json:"quarantined"` // always true; stated for operators
	Cases       []SynthCase `json:"cases"`
}

// SynthesizeCases generates eval cases grounded in knowledgebase
// passages and lands them in the corpus quarantined (provenance
// "synthetic"). The gating pool is untouched until promote-cases.
func (e *Engine) SynthesizeCases(ctx context.Context, artifactRef string, count int) (*SynthResult, error) {
	if !e.casegen.Configured() {
		return nil, ErrNoCaseGen
	}
	if count < 1 {
		count = 1
	}

	var loadResp struct {
		Artifact Artifact `json:"artifact"`
	}
	if err := e.store.Call(ctx, "load",
		map[string]any{"ref": artifactRef}, &loadResp); err != nil {
		return nil, err
	}

	knowledge := e.knowledge(ctx, artifactRef)
	if len(knowledge) == 0 {
		return nil, fmt.Errorf("%w (artifact %s)", ErrNoKnowledge, artifactRef)
	}

	// Existing cases travel as style references (any split).
	var casesResp struct {
		Cases []Case `json:"cases"`
	}
	if err := e.corpus.Call(ctx, "cases",
		map[string]any{"artifact_ref": artifactRef}, &casesResp); err != nil {
		return nil, err
	}
	examples := make([]map[string]any, 0, maxSynthExamples)
	for _, c := range casesResp.Cases {
		if len(examples) == maxSynthExamples {
			break
		}
		examples = append(examples, map[string]any{
			"input": c.Input, "expected": c.Expected,
		})
	}

	var synthResp struct {
		Cases []SynthCase `json:"cases"`
	}
	if err := e.casegen.Call(ctx, "synth", map[string]any{
		"artifact":  loadResp.Artifact,
		"knowledge": knowledge,
		"examples":  examples,
		"count":     count,
	}, &synthResp); err != nil {
		return nil, err
	}

	result := &SynthResult{
		ArtifactRef: artifactRef,
		Generated:   len(synthResp.Cases),
		Quarantined: true,
		Cases:       []SynthCase{},
	}
	if len(synthResp.Cases) == 0 {
		e.logf("synthesis dry: adapter returned no cases")
		return result, nil
	}

	addCases := make([]map[string]any, 0, len(synthResp.Cases))
	for _, c := range synthResp.Cases {
		id := synthID(c.Input, c.Expected)
		addCases = append(addCases, map[string]any{
			"id": id, "input": c.Input, "expected": c.Expected,
			"split": "train", "source": "synthetic", "quarantined": true,
		})
		c.ID = id
		result.Cases = append(result.Cases, c)
	}

	var addResp struct {
		Added      int `json:"added"`
		Duplicates int `json:"duplicates"`
	}
	if err := e.corpus.Call(ctx, "add-cases", map[string]any{
		"artifact_ref": artifactRef,
		"cases":        addCases,
	}, &addResp); err != nil {
		return nil, err
	}
	result.Added = addResp.Added
	result.Duplicates = addResp.Duplicates
	e.logf("synthesized %d case(s): %d added (quarantined), %d duplicate(s)",
		result.Generated, result.Added, result.Duplicates)
	return result, nil
}

// PromoteCases clears quarantine on reviewed cases.
func (e *Engine) PromoteCases(ctx context.Context, artifactRef string, ids []string) (promoted int, missing []string, err error) {
	var resp struct {
		Promoted int      `json:"promoted"`
		Missing  []string `json:"missing"`
	}
	if err := e.corpus.Call(ctx, "promote-cases", map[string]any{
		"artifact_ref": artifactRef, "ids": ids,
	}, &resp); err != nil {
		return 0, nil, err
	}
	return resp.Promoted, resp.Missing, nil
}

// synthID derives a deterministic content id for a synthesized case.
func synthID(input, expected string) string {
	sum := sha256.Sum256([]byte(input + "\x00" + expected))
	return "syn-" + hex.EncodeToString(sum[:])[:12]
}
