package engine

import "math/rand"

// Significance testing for the acceptance gate: a paired bootstrap on
// per-case mean scores (candidate vs baseline over the same cases).
//
// The pairing unit is the CASE — trials collapse into a per-case mean
// first, so trial noise is averaged out and the resample respects the
// case as the independent observation.

const (
	// sigMinPairs is the floor below which significance testing is
	// disabled: with fewer paired cases the bootstrap is vacuous
	// (resamples repeat the same handful of diffs), so the gate falls
	// back to mean-only and says so on the log.
	sigMinPairs = 8

	// sigResamples is the bootstrap resample count.
	sigResamples = 10000
)

// perCaseMeans collapses trial-level scores into one mean per case id.
func perCaseMeans(scores []CaseScore) map[string]float64 {
	sums := make(map[string]float64)
	counts := make(map[string]int)
	for _, s := range scores {
		sums[s.CaseID] += s.Score
		counts[s.CaseID]++
	}
	means := make(map[string]float64, len(sums))
	for id, sum := range sums {
		means[id] = sum / float64(counts[id])
	}
	return means
}

// pairedDiffs returns candidate−baseline per-case mean differences over
// the cases both sides scored. Order is unspecified; the bootstrap is
// order-invariant.
func pairedDiffs(baseline, candidate []CaseScore) []float64 {
	base := perCaseMeans(baseline)
	cand := perCaseMeans(candidate)
	diffs := make([]float64, 0, len(base))
	for id, b := range base {
		if c, ok := cand[id]; ok {
			diffs = append(diffs, c-b)
		}
	}
	return diffs
}

// bootstrapP estimates a one-sided p-value for H0 "no improvement"
// (true mean difference ≤ 0) via a paired bootstrap: resample the
// per-case diffs with replacement and count resamples whose mean fails
// to improve. Deterministic under a fixed seed.
//
// p = (1 + #{mean(d*) ≤ 0}) / (resamples + 1)  — the +1 smoothing keeps
// p > 0 so a finite resample count never claims impossible certainty.
func bootstrapP(diffs []float64, resamples int, seed int64) float64 {
	if len(diffs) == 0 {
		return 1
	}
	rng := rand.New(rand.NewSource(seed)) // #nosec G404 -- statistical resampling, not crypto
	n := len(diffs)
	atOrBelowZero := 0
	for r := 0; r < resamples; r++ {
		var sum float64
		for i := 0; i < n; i++ {
			sum += diffs[rng.Intn(n)]
		}
		if sum <= 0 { // mean ≤ 0 ⇔ sum ≤ 0
			atOrBelowZero++
		}
	}
	return float64(1+atOrBelowZero) / float64(resamples+1)
}
