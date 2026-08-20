package engine

import "testing"

func caseScores(vals map[string]float64) []CaseScore {
	out := make([]CaseScore, 0, len(vals))
	for id, v := range vals {
		out = append(out, CaseScore{CaseID: id, Score: v})
	}
	return out
}

func TestPerCaseMeansCollapsesTrials(t *testing.T) {
	scores := []CaseScore{
		{CaseID: "a", Score: 0.4}, {CaseID: "a", Score: 0.6},
		{CaseID: "b", Score: 1.0},
	}
	means := perCaseMeans(scores)
	if means["a"] != 0.5 || means["b"] != 1.0 {
		t.Errorf("means = %v, want a=0.5 b=1.0", means)
	}
}

func TestPairedDiffsIntersects(t *testing.T) {
	base := caseScores(map[string]float64{"a": 0.5, "b": 0.5, "only-base": 0.2})
	cand := caseScores(map[string]float64{"a": 0.9, "b": 0.4, "only-cand": 1.0})
	diffs := pairedDiffs(base, cand)
	if len(diffs) != 2 {
		t.Fatalf("diffs = %v, want the 2 shared cases only", diffs)
	}
	var sum float64
	for _, d := range diffs {
		sum += d
	}
	if got := sum; got < 0.299 || got > 0.301 { // +0.4 and -0.1
		t.Errorf("diff sum = %v, want 0.3", got)
	}
}

func TestBootstrapClearImprovementIsSignificant(t *testing.T) {
	diffs := make([]float64, 10)
	for i := range diffs {
		diffs[i] = 0.2
	}
	if p := bootstrapP(diffs, sigResamples, 1); p > 0.01 {
		t.Errorf("p = %v for uniform +0.2 diffs, want tiny", p)
	}
}

func TestBootstrapNoiseIsInsignificant(t *testing.T) {
	// Mean is barely positive but half the cases regress.
	diffs := []float64{0.5, 0.5, 0.5, 0.5, -0.3, -0.3, -0.3, -0.3}
	if p := bootstrapP(diffs, sigResamples, 1); p < 0.2 {
		t.Errorf("p = %v for noisy diffs, want large", p)
	}
}

func TestBootstrapDeterministicUnderSeed(t *testing.T) {
	diffs := []float64{0.3, -0.1, 0.2, 0.05, -0.02, 0.4, 0.1, -0.2}
	p1 := bootstrapP(diffs, sigResamples, 42)
	p2 := bootstrapP(diffs, sigResamples, 42)
	if p1 != p2 {
		t.Errorf("same seed gave p=%v then p=%v", p1, p2)
	}
}

func TestBootstrapDegenerateInputs(t *testing.T) {
	if p := bootstrapP(nil, sigResamples, 1); p != 1 {
		t.Errorf("p = %v for no diffs, want 1", p)
	}
	zeros := []float64{0, 0, 0, 0, 0, 0, 0, 0}
	if p := bootstrapP(zeros, sigResamples, 1); p < 0.99 {
		t.Errorf("p = %v for all-zero diffs, want ≈1 (no improvement)", p)
	}
}
