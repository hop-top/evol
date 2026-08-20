# evol

[![CI](https://github.com/hop-top/evol/actions/workflows/ci-go.yml/badge.svg)](https://github.com/hop-top/evol/actions/workflows/ci-go.yml)
[![12-factor AI-CLI](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/hop-top/evol/main/.12fcc.json)](https://github.com/hop-top/evol/blob/main/.12fcc.json)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Self-improvement loop for agent capabilities. evol takes an artifact an
agent works from — a skill, a prompt, a command, a tool config —
evaluates it against real cases, proposes candidate revisions, executes
and scores them, gates the results against a baseline, and promotes or
rejects. Every verdict, accepted and rejected alike, is written back to
a corpus so the next generation starts smarter.

> **Status: pre-alpha.** The port contracts in [spec/](spec/) are
> INTERNAL DRAFT — unstable until a first end-to-end improvement is
> demonstrated. Nothing here is a stable interface yet.

## How it works

```
             ┌────────────────────────────────────────────────┐
             │                loop engine (evol)              │
             └────────────────────────────────────────────────┘
   load artifact → eval cases → propose → execute → score → gate
        │              │           │         │        │       │
   ArtifactStore    Corpus     Generator  Executor  Scorer  accept ⇒ write + record
                      ▲                                      reject ⇒ record (tabu)
                      └────────── write-back, every generation ─────────┘
```

The engine owns control flow only. Every I/O exchange crosses a
**port** — a versioned JSON contract spoken over process boundaries.
Implementations are **adapters**: standalone executables in any
language. The engine never links adapter code.

| Port | Purpose | Reference adapter |
|------|---------|-------------------|
| ArtifactStore | load/write/version the artifact under evolution | [fs-artifact](adapters/fs-artifact/) — skills directory |
| Generator | propose candidate revisions | [generator-llm](adapters/generator-llm/) — LLM mutation strategies, provider URIs |
| Executor | run a candidate against an eval case | [executor-apx](adapters/executor-apx/) — subprocess, +cassette replay, +profile isolation |
| Corpus | eval cases, verdicts, tabu history, corrections | [corpus-fs](adapters/corpus-fs/) — file-backed |
| KnowledgeBase | contextual knowledge for proposals (optional) | [ctxt-kb](adapters/ctxt-kb/) — degrades gracefully |
| Scorer *(draft)* | score a transcript against a case | [scorer-eva](adapters/scorer-eva/) — contract-based checks |
| Gate *(draft)* | benchmark regression check | [ben-gate](adapters/ben-gate/) |

Tier-2 ports (Scorer, Gate, Reviewer, Audit) are deliberately not in
`spec/` yet — they get extracted from the working implementation, not
designed up front. Current draft shapes: [docs/scorer-draft.md](docs/scorer-draft.md),
[adapters/ben-gate/README.md](adapters/ben-gate/README.md).

## The two seams that matter

**Ports are JSON over stdio.** One request object on stdin, one
response on stdout, non-zero exit means adapter error. Any language, no
SDK. An adapter for your own knowledge base, corpus store, or optimizer
is one small program away — see [spec/README.md](spec/README.md) for
the wire protocol.

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
(LLM pipes) — each roughly fifteen lines. **No tool is privileged**:
bring any runner by writing one shim.

## Quick start

```sh
git clone https://github.com/hop-top/evol && cd evol
mise trust && mise run install

# engine + adapters
go build -o e2e/bin/evol .
for a in fs-artifact generator-llm executor-apx corpus-fs scorer-eva; do
  go build -o e2e/bin/$a ./adapters/$a
done

# verify wiring — no LLM calls
e2e/bin/evol run --config e2e/evol.yaml --dry-run --format json
```

The repository ships a complete example under [e2e/](e2e/): a
deliberately mediocre commit-message skill, twelve golden cases, a
scoring contract, and a runbook. [e2e/RUNBOOK.md](e2e/RUNBOOK.md) walks
the live loop end to end.

**Local models are a first-class path.** The generator takes provider
URIs (`ollama://llama3.2:3b?base_url=http://localhost:11434`,
`anthropic://claude-sonnet-5`, and others via `hop.top/kit`'s LLM
layer), and runners can point at local backends the same way — the
whole loop can run without an API key.

Exit codes for `evol run`: `0` promoted · `1` no improvement ·
`2` gate precondition failed · `3` config or adapter error.

## Ground rules

- **Write-back is engine behavior, not adapter courtesy.** Every
  candidate and verdict lands in the corpus. A loop without memory is
  re-rolling, not self-improving.
- **Determinism is the executor implementation's promise, not the
  interface's.** The reference executor can freeze the environment with
  recorded cassettes and isolate candidates in throwaway profiles; a
  plain subprocess is valid but weaker.
- **Degrade gracefully.** Optional ports report
  `{"unavailable": true}`; the engine continues with reduced
  capability.
- **Model choice is data.** The provider that produced each candidate
  is recorded with it — model comparison is part of the eval space, not
  a config afterthought.

## Layout

```
spec/        port contracts (wire protocol, one file per port)
adapters/    reference adapters, one directory per port implementation
internal/    loop engine + port client
e2e/         runnable example: artifact, cases, contract, runbook
docs/        engine-level drafts pending extraction to spec/
```

## Development

```sh
mise trust && mise run install
go build ./... && go test ./...
golangci-lint run ./...
```

## License

[MIT](LICENSE). Maintained by Jad Bitar.
