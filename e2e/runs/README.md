# Run evidence

Committed proof of verified evolution runs — the artifacts the
[spec publication](../../spec/README.md) rests on. Runtime state
(corpus, cassette cache) is gitignored; what lands here is the durable
record of a run worth citing.

| File | What it is |
|------|------------|
| [gen1-improvement.json](gen1-improvement.json) | final summary of the first verified promotion: `commit-messages/SKILL.md` baseline `7e0c639e` → promoted `1d61f60b`, holdout mean 0.705 → 0.824, p = 0.0002, 3 candidates in 1 generation, engine exit 0 |
| [gen1-generations.jsonl](gen1-generations.jsonl) | the full generation ledger for that run — every candidate with its strategy, rationale, scores, and verdict, accepted and rejected alike (write-back is engine behavior; a loop without memory is re-rolling) |

The promoted skill itself is committed at
[../artifacts/commit-messages/SKILL.md](../artifacts/commit-messages/SKILL.md);
its recorded holdout interactions are frozen under
[../fixtures/cassettes/](../fixtures/cassettes/) and re-verified by CI
on every touch (see the [RUNBOOK](../RUNBOOK.md) regression-gate
section).
