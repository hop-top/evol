# Reviewing the eval pool

The corpus quarantines machine-generated intake (synthetic, mined)
until a human clears it; human corrections skip quarantine entirely.
Three verbs cover the flow:

| Verb | Purpose |
|------|---------|
| `evol cases list --artifact <ref> [--quarantined\|--all]` | inspect the gating pool and the intake queue |
| `evol cases promote --artifact <ref> --ids <id,...>` | clear quarantine on reviewed cases |
| `evol cases correct --artifact <ref> --case-id <id> --input ... [--expected ...] [--split train\|holdout]` | record a human correction |

Corrections are served by the corpus `corrections` action and merged
into the gating pool at the next eval-set build — the run log shows
`corrections: merged N of M into the "<split>" pool`, and each
candidate's corpus record carries the correction's per-case score.

Review discipline: quarantined synthetic cases must be read before
promotion — the circular-eval guard keeps generation grounded in the
knowledge base, but only a human can judge whether a case tests
something worth wanting.
