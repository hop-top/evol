# evol

[![CI](https://github.com/hop-top/evol/actions/workflows/ci-go.yml/badge.svg)](https://github.com/hop-top/evol/actions/workflows/ci-go.yml)
[![12-factor AI-CLI](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/hop-top/evol/main/.12fcc.json)](https://github.com/hop-top/evol/blob/main/.12fcc.json)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**You have a skill file your agent works from. You think it's mediocre.
You rewrite it. Is it better?**

Most answers to that question are vibes. You read the diff, it reads
nicer, you commit. Sometimes you A/B it against a handful of prompts and
call the higher number a win — on five cases, one trial each, with no
notion of whether the gap survives resampling.

evol is the loop that answers it properly, and its main job is telling
you **no**. Across five recorded live runs it judged 30 candidate
revisions and rejected 26. Of the four it accepted, the first was a text
reflow that beat the baseline mean on trial noise — the gate at the time
checked the mean and nothing else, a human caught it in the diff, and
the loop grew a paired-bootstrap significance test so it can't recur.
The other three each cleared both conditions.

That is the product. Not "it improves your prompts" — **it can tell
whether anything improved.**

## The result it has produced

One artifact, one domain, fully reproducible:

| | |
|---|---|
| Artifact | `commit-messages/SKILL.md` (deliberately mediocre, in `e2e/artifacts/`) |
| Baseline holdout mean | **0.7049** |
| Promoted holdout mean | **0.8236** (+17% relative) |
| Significance | **p = 0.0002**, seeded paired bootstrap |
| Eval | 8 holdout cases × 3 trials |
| Verdict | diff-inspected: a genuine semantic upgrade, not a reflow |

Evidence is `e2e/runs/gen1-improvement.json`; the full generation
history, rejects included, is `e2e/runs/gen1-generations.jsonl`. Cite
those, not this paragraph.

The ablation matters more than the headline. Runs 1–4 proposed
twenty-seven candidates from the artifact text alone; the only one that
ever passed was the reflow that passed on noise. Run 5 changed exactly
one variable — candidate proposals were grounded in retrieved knowledge
through the KnowledgeBase port — and **all three** of its candidates
cleared the full gate, at p = 0.0002, 0.0041, and 0.0009.
**Grounding, not scale.**

> **Status: pre-alpha code, published spec.** The port contracts in
> [spec/](spec/) are published v1 (`evol: "1"`, additive-only evolution
> — the commitment is in [spec/publishing.md](spec/publishing.md)),
> released only after the loop demonstrated the verified improvement
> above. The CLI and reference adapters remain pre-alpha: flags and
> layouts may change. There is no installable release yet.

## What breaks without this

If you are building an agent self-improvement loop, these are the
failure modes evol exists to close. Each one is a real bug found in a
real system, including this one:

- **The loop that can't improve anything.** An optimizer that mutates
  the wrong field and writes back output identical to its input. Every
  run reports success. Nothing changed.
- **Winning on noise.** "Any improvement > 0" on five holdout rows is
  a coin flip with extra steps. evol's run 2 promoted a reflow this way
  before the significance gate existed.
- **Grading your own homework.** Synthetic eval cases generated from
  the artifact's own text reward the artifact's existing blind spots.
  evol refuses to synthesize without knowledge-base grounding, and
  quarantines what it does generate until a human promotes it.
- **A loop with no memory.** Regenerate the eval set each run, discard
  the rejects, and the generator re-proposes last week's failure
  forever. evol writes every verdict back — accepted and rejected — and
  feeds the reject history to the generator as a tabu list.
- **Untrustworthy A/B.** Baseline and candidate sharing one process,
  one environment, one set of live API calls that answer differently
  each time. evol runs each candidate in a throwaway profile and can
  freeze the whole environment behind recorded cassettes.
- **Silent decay after promotion.** Nothing re-checks a promoted
  artifact. evol commits a cassette-backed regression gate that runs in
  CI with zero API keys.

## How it works

```
             ┌────────────────────────────────────────────────┐
             │                loop engine (evol)              │
             └────────────────────────────────────────────────┘
   load artifact → eval cases → propose → execute → score → gate
        │              │           │         │        │       │
   ArtifactStore    Corpus     Generator  Executor  Scorer  accept ⇒ write + record
                      ▲          ▲                           reject ⇒ record (tabu)
                      │     KnowledgeBase                            │
                      └────────── write-back, every generation ──────┘
                                                          Audit ⇒ run ledger
```

The engine owns control flow only. Every I/O exchange crosses a
**port** — a versioned JSON contract spoken over process boundaries.
Implementations are **adapters**: standalone executables in any
language. The engine never links adapter code.

### The gate

A candidate is promoted only if **both** hold:

```
mean(candidate) ≥ mean(baseline) + delta        # thresholds.delta
paired_bootstrap_p ≤ sig_level                  # thresholds.sig_level, default 0.05
```

A one-sided paired bootstrap over 10,000 resamples, seeded
(`thresholds.sig_seed`, default 1) so p-values reproduce exactly. The
pairing unit is the **case**, not the trial: trials collapse to a
per-case mean first, so running the same case more times cannot
manufacture significance. Below **8 paired cases** the test is
automatically disabled and the run falls back to mean-only gating with a
logged warning — it refuses to pretend a p-value from five samples means
anything. A candidate that clears the mean but fails significance is
rejected, with that rationale recorded.

### Ports

Six contracts are published v1 in [spec/](spec/); Audit is a draft
introduced after publication.

| Port | Purpose | Reference adapters |
|------|---------|--------------------|
| [ArtifactStore](spec/port-artifactstore.md) | load / write / version the artifact | [fs-artifact](adapters/fs-artifact/) — git-native versioning + restore |
| [Generator](spec/port-generator.md) | propose candidate revisions | [generator-llm](adapters/generator-llm/) — mutation strategies, tabu-aware, provider URIs |
| [Executor](spec/port-executor.md) | run a candidate against an eval case | [executor-apx](adapters/executor-apx/) — subprocess, +cassette replay, +profile isolation |
| [Corpus](spec/port-corpus.md) | cases, verdicts, tabu history, corrections | [corpus-fs](adapters/corpus-fs/) — file-backed |
| [Scorer](spec/port-scorer.md) | score a transcript against a case | [scorer-eva](adapters/scorer-eva/); the e2e example uses a checked-in Python scorer |
| [KnowledgeBase](spec/port-knowledgebase.md) | grounding for proposals + synthesis *(optional)* | [ctxt-kb](adapters/ctxt-kb/), plus a [third-party Python adapter](examples/third-party/obsidian-kb/) |
| [Audit](spec/port-audit.md) *(draft)* | run ledger *(optional)* | [audit-tlc](adapters/audit-tlc/) (tracker-backed), [audit-fs](adapters/audit-fs/) (file-backed) |

Supporting adapters, same wire protocol: [ben-gate](adapters/ben-gate/)
(benchmark regression gate), [casegen-llm](adapters/casegen-llm/)
(grounded case synthesis), [cases-crtx](adapters/cases-crtx/) (mine eval
cases from recorded agent sessions), [routing-emit](adapters/routing-emit/)
(model-routing config from recorded evidence), and
[runner-xrr](adapters/runner-xrr/) (cassette record/replay wrapper).

## The two seams that matter

**Ports are JSON over stdio.** One request object on stdin, one response
on stdout, non-zero exit means adapter error. Any language, no SDK. The
proof is [examples/third-party/obsidian-kb/](examples/third-party/obsidian-kb/):
a KnowledgeBase adapter in one file of standard-library Python, written
against the spec text alone with zero evol code imported, plus six
conformance fixture triples. Contract-read to working adapter: roughly
one sitting. Its author also logged five genuine spec ambiguities they
hit — unset-config semantics, envelope echo, score scale, `append`
targeting, trailing newlines — and those are open feedback, not yet
resolved in the contracts.

**The runner contract makes the agent swappable.** The system under
evolution is whatever runs the artifact — an agent CLI, an LLM pipe,
your own harness. The reference executor spawns any command conforming
to a four-line contract:

```
stdin   case input
env     EVOL_CANDIDATE_REF  path to the candidate artifact body
        EVOL_PROVIDER       optional model URI; interpretation is the runner's
stdout  agent output only
exit≠0  run failure (recorded as data, not an adapter error)
```

Reference shims live in `e2e/bin/runners/` — `claude`, `codex`,
`gemini`, `opencode` (agent CLIs) and `foo`, `fabric`, `llm`, `ollama`
(LLM pipes) — each roughly fifteen lines. Four are smoke-tested live;
four are honestly marked untested. **No tool is privileged**: bring any
runner by writing one shim.

## Quick start

```sh
git clone https://github.com/hop-top/evol && cd evol
mise trust && mise run install

# engine + adapters the example config uses
# (run under mise: it sets GOFLAGS=-buildvcs=false, needed in worktrees)
mise exec -- go build -o e2e/bin/evol .
for a in fs-artifact generator-llm executor-apx corpus-fs ctxt-kb; do
  mise exec -- go build -o e2e/bin/$a ./adapters/$a
done

# verify wiring — no LLM calls, no keys
e2e/bin/evol run --config e2e/evol.yaml --dry-run --format json

# what's evolvable, and how each artifact has fared so far
e2e/bin/evol targets --config e2e/evol.yaml
```

The repository ships the complete worked example under [e2e/](e2e/):
the mediocre commit-message skill, 16 golden cases (8 train / 8
holdout), a scoring contract, eight runner shims, and committed
regression fixtures. [e2e/RUNBOOK.md](e2e/RUNBOOK.md) walks the live
loop end to end, including calibration — the eval is tuned so a
baseline lands ~0.63–0.72 and a well-written skill reaches ~0.91. If you
swap the agent-under-test model, re-run `e2e/bin/calibrate.sh`; an
uncalibrated eval silently makes the gate either unreachable or trivial.

**Local models are a first-class path.** The generator takes provider
URIs (`ollama://llama3.2:3b?base_url=http://localhost:11434`,
`anthropic://claude-sonnet-5`, and others via `hop.top/kit`'s LLM
layer), and runners can point at local backends the same way — the whole
loop can run without an API key.

## CLI

```sh
evol run                      # one evolution run; --artifact or --select, --dry-run
evol targets                  # what's evolvable + per-artifact history
evol cases synth              # grounded synthetic cases (quarantined)
evol cases list               # review the pool, quarantined or all
evol cases promote            # human promotes quarantined cases into gating
evol cases correct            # write a human correction into the pool
evol rollback                 # restore a previous artifact version
evol runs list | show <id>    # read the audit ledger
evol routing emit             # model-routing config from recorded evidence
```

Exit codes for `evol run`: `0` promoted · `1` no improvement ·
`2` gate precondition failed · `3` config or adapter error. Other verbs
reuse the same codes; notably `cases synth` exits `2` when the knowledge
base yields no grounding — the circular-eval guard refusing to invent
cases from the artifact alone.

On promotion, a configurable hook (`promotion.hook`) hands off to any
publisher with `EVOL_PROMOTED_REF`, `EVOL_PROMOTED_VERSION`, and
`EVOL_PROMOTED_GIT_COMMIT` in the environment. Setting
`EVOL_ARTIFACT_GIT=1` makes promotions and rollbacks git-native. The
hook is a CLI concern, not an engine one: the engine's contract ends at
"artifact written, corpus recorded" — what happens next is operator
policy. See [docs/promotion.md](docs/promotion.md).

Without `--artifact`, `evol run` picks its own target by explicit
policy: `--select never-run | worst | stale | drift | kb-churn`. `drift`
chases the most negative score trend across recent generations;
`kb-churn` chases artifacts whose grounding knowledge moved since the
last evolution — on real KB timestamps, with a documented four-rung
degrade ladder when that signal is absent. A cron firing
`--select kb-churn` is a loop that schedules itself against world
evidence. See [docs/self-scheduling.md](docs/self-scheduling.md).

## Ground rules

- **Write-back is engine behavior, not adapter courtesy.** Every
  candidate and verdict lands in the corpus. A loop without memory is
  re-rolling, not self-improving.
- **The loop must not grade its own homework.** Synthetic cases are
  refused without knowledge grounding and quarantined until a human
  promotes them.
- **Degrade, never fabricate.** Optional ports report
  `{"unavailable": true}` and the engine continues with reduced
  capability. A missing signal produces a documented proxy ladder, never
  invented data.
- **A scorer that cannot score must fail loudly.** Most ports have two
  failure planes (adapter error vs. recorded run failure); scorers get
  one, because a fabricated number corrupts every downstream verdict.
- **Determinism is the executor implementation's promise, not the
  interface's.** The reference executor can freeze the environment with
  recorded cassettes and isolate candidates in throwaway profiles; a
  plain subprocess is valid but weaker.
- **Model choice is data.** The provider that produced each candidate is
  recorded with it — model comparison is part of the eval space, not a
  config afterthought.
- **Nothing ships on tests alone.** Every capability here has been
  exercised against real binaries and real models at least once. Bugs
  found *only* that way: stale adapter binaries silently serving old
  contracts, cassette identity keyed on reassembled instead of source
  content, a relevance ranker letting a 10-token note outrank the
  authoritative one.

## Layout

```
spec/        port contracts, published v1 (wire protocol, one file per port)
adapters/    reference adapters, one directory per implementation
cmd/         CLI verbs
internal/    loop engine, gating, significance, target selection, port client
e2e/         runnable example: artifact, cases, contract, runbook, fixtures
examples/    third-party adapter proof (zero-dep Python KnowledgeBase)
docs/        per-capability notes, each with honest limitations
```

Capability docs: [self-scheduling](docs/self-scheduling.md) ·
[synthesis](docs/synthesis.md) · [review](docs/review.md) ·
[promotion](docs/promotion.md) · [audit](docs/audit.md) ·
[routing write-back](docs/routing-writeback.md)

## Known limitations

- No released, installable version. Blocked on an upstream tag-shape
  question; `go install` is not yet a supported path.
- The scorer's LLM-judge tier is code-complete upstream but not landed —
  scoring today is programmatic and contract-based.
- Session mining is converter-only: `cases-crtx` turns recorded session
  envelopes into eval cases, but no live capture pipeline feeds it.
- Conformance fixtures exist only for KnowledgeBase. House rule: a port
  gets fixtures once a *second* real adapter exists for it
  ([spec/conformance-plan.md](spec/conformance-plan.md)).
- The file-backed corpus documents itself as the interim implementation;
  an indexed successor belongs behind the same port.
- **The claim here is narrow.** One artifact, one domain, one verified
  improvement. Do not read it as "evol improves agents." Read it as
  "evol measured one improvement and rejected twenty-two non-improvements."

## Development

```sh
mise trust && mise run install
go build ./... && go test ./...
golangci-lint run ./...
```

## License

[MIT](LICENSE). Maintained by Jad Bitar.
