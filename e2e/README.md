# End-to-end example: evolving a commit-messages skill

This tree serves two purposes at once: it is the repo's regression
suite, and it is a complete worked example of the loop — real config,
real adapters, real eval cases, and the committed evidence of a real
promotion. Everything here runs from the repo root.

What it demonstrates (generation 1, verified): the loop took a
deliberately mediocre skill from a holdout mean of **0.705 to 0.824**
(+17% relative) in one generation, three candidates tried, paired
significance p = 0.0002 — then wrote the promoted skill back and armed
a CI replay gate behind it. The run evidence lives in
[runs/](runs/README.md); the [port contracts](../spec/README.md) cite
it as their publication precondition.

To run it yourself, start at the [RUNBOOK](RUNBOOK.md) — build, seed,
dry-run, live run, cassette recording, regression gate.

## Layout

| Path | State | What it is |
|------|-------|------------|
| [RUNBOOK.md](RUNBOOK.md) | committed | step-by-step operation: build, seed, run, record, regress |
| [evol.yaml](evol.yaml) | committed | loop config wiring every port to a built adapter |
| [artifacts/](artifacts/) | committed | the artifact under evolution (`commit-messages/SKILL.md`, promoted content). Only artifacts here — the store serves every file under this root, so even a README would show up in `evol targets` |
| [cases/](cases/) | committed | eval cases (`cases.jsonl`), train/holdout split |
| [contracts/](contracts/) | committed | eva scoring contract (`commit-subject.yaml`); `bin/score-commit.py` mirrors it stdlib-only |
| [bin/](bin/) | scripts committed | seed/calibrate/regress helpers; built engine + adapter binaries land here gitignored |
| [bin/runners/](bin/runners/) | committed | one runner shim per agent CLI — the agent under test is a swappable seam (see RUNBOOK "Runner selection") |
| `cassettes/` | gitignored | runtime record cache (`XRR_MODE=record`) — live calls persisted as they happen |
| [fixtures/cassettes/](fixtures/cassettes/) | committed | frozen regression fixtures CI replays — no agent calls, no keys |
| [regress-baseline.json](regress-baseline.json) | committed | armed per-case baseline scores for the replay-diff gate |
| [runs/](runs/README.md) | committed | evidence of verified runs — summaries and full generation ledgers |
| `.corpus/` | gitignored | seeded corpus (cases, verdicts, tabu) keyed by artifact-ref hash |
