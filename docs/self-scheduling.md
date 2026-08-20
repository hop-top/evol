# Self-scheduling

The loop picks its own next target when `evol run` gets a `--select`
policy instead of an `--artifact` ref. Self-scheduling is nothing more
than running that on a schedule:

```sh
# cron / launchd / CI schedule — no daemon, no state beyond the corpus
evol run --config evol.yaml --select drift
```

`evol targets` shows the signals every policy reads.

## Policy semantics

| Policy | Picks | Signal |
|---|---|---|
| `never-run` (default) | first artifact with no history | corpus history presence |
| `worst` | lowest last best score | corpus history |
| `stale` | fewest recorded generations | corpus history (generation count is the staleness proxy — the corpus stores no wall-clock) |
| `drift` | most negative recent score trend | per-generation best scores: `mean(last min(3, n-1)) - mean(prior)`; artifacts with fewer than two generations carry no trend and rank last; falls back to `never-run` when nothing has a trend |
| `kb-churn` | most attention-starved | never-evolved first, then fewest generations |

Ties always break by ref, so a given corpus state selects the same
target every time.

## The kb-churn caveat, honestly

The intended signal is "knowledge newer than the artifact's last
evolution" — a knowledge-base edit should re-queue the artifacts it
informs. The KnowledgeBase port carries no timestamps today, so that
cannot be measured without faking it. v0 therefore uses an
attention-starvation proxy (never-evolved, then fewest generations).

Proposed port addition (not yet in spec/):

```
action "newest" {query} → {ts}   # RFC3339 of the newest passage
                                 # matching the query; {unavailable:true}
                                 # degrades like every KB action
```

With that action, `kb-churn` becomes: select artifacts whose
`kb.newest(ref) >` timestamp of their last recorded generation — which
also requires the corpus to start recording generation timestamps
(deliberately omitted so far for replay determinism; revisit together).

## Interaction with budgets

A scheduled `evol run` spends real generator and executor calls. Pair
the schedule with the loop's own budget (`budget.generations`,
`budget.max_candidates`) and recorded-environment replay so repeated
(candidate, case) pairs cost nothing.
