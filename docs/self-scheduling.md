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
| `kb-churn` | knowledge moved since last evolution | KB `newest(ref)` vs the last generation's `recorded_at`; most-recent knowledge first, then the degrade ladder below |

Ties always break by ref, so a given corpus state selects the same
target every time.

## kb-churn v1: the real signal, with a degrade ladder

The signal is "knowledge newer than the artifact's last evolution" — a
knowledge-base edit re-queues the artifacts it informs. Two additions
made it measurable without faking anything: the KnowledgeBase optional
`newest {query} → {ts}` action (spec/port-knowledgebase.md) and corpus
record rows carrying `recorded_at` (spec/port-corpus.md; the engine
stamps them, nothing fingerprints them, and tests inject a fixed clock).

Selection walks a ladder — each rung only when the one above has no
answer:

| Rung | Picks | When it applies |
|---|---|---|
| 1 | artifacts with `kb.newest(ref) > last generation recorded_at`, most-recent knowledge first | KB serves `newest` AND the artifact has stamped history |
| 2 | clean never-evolved artifacts | no measurable churn anywhere |
| 3 | history-degraded artifacts (corpus could not answer) | no clean never-run rows |
| 4 | fewest recorded generations (the v0 attention proxy) | everything has history, nothing shows churn |

Every failure mode degrades one rung — KB unconfigured, `newest`
unsupported (older adapters), null `ts` (knowledge exists but nothing is
timestamped), unstamped history rows — and ties always break by ref.
`evol targets --format json` exposes the raw pair per row as
`kb_newest` and `last_evolved`; the table shows LAST-EVOLVED.

## Interaction with budgets

A scheduled `evol run` spends real generator and executor calls. Pair
the schedule with the loop's own budget (`budget.generations`,
`budget.max_candidates`) and recorded-environment replay so repeated
(candidate, case) pairs cost nothing.
