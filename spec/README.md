# evol port contracts

> **STATUS: INTERNAL DRAFT** — contracts are unstable until a first
> end-to-end improvement is demonstrated. Field names, envelope shape,
> and action sets may change without notice. No conformance fixtures
> ship until a second real adapter exists for any given port.

evol is a self-improvement loop for agent capabilities: it evaluates an
artifact (a skill, prompt, command, or tool config), proposes candidate
revisions, executes them against eval cases, scores the results, gates
them against a baseline, and promotes or rejects — writing every verdict
back to a corpus so the next generation starts smarter.

The loop engine owns control flow only. Every I/O exchange crosses a
**port**: a versioned JSON contract spoken over process boundaries.
Implementations are **adapters** — standalone executables in any
language. The engine never links adapter code.

## Wire protocol

- Transport: JSON over stdio. One request object on stdin, one response
  object on stdout. One invocation per action.
- Non-zero exit = adapter error. stdout is then ignored; stderr carries
  diagnostics.
- Every request and response envelope carries:

| Field | Type | Notes |
|-------|------|-------|
| `evol` | string | contract version; always `"1"` for this draft |
| `port` | string | port name, e.g. `"generator"` |
| `action` | string | action name, e.g. `"propose"` |

Request payload fields sit alongside the envelope fields; response
payloads likewise. All field names are `snake_case`.

```json
{"evol": "1", "port": "corpus", "action": "tabu", "artifact_ref": "skills/commit-style"}
```

## Ports

| Port | Purpose | Tier | Contract |
|------|---------|------|----------|
| ArtifactStore | load/write/version the artifact under evolution | 1 | [port-artifactstore.md](port-artifactstore.md) |
| Generator | propose candidate revisions | 1 | [port-generator.md](port-generator.md) |
| Executor | run a candidate against an eval case | 1 | [port-executor.md](port-executor.md) |
| Corpus | eval cases, verdicts, tabu history, corrections | 1 | [port-corpus.md](port-corpus.md) |
| KnowledgeBase | contextual knowledge for target selection and proposals | 1 | [port-knowledgebase.md](port-knowledgebase.md) |

Tier 2 ports (Scorer/Judge, Gate, Reviewer, Audit) are deliberately not
specified yet: they will be extracted from the working reference
implementation, not designed up front.

## Ground rules

- **Write-back is engine behavior, not adapter courtesy.** Every
  candidate and its verdict — accepted and rejected alike — is recorded
  through the Corpus port. A loop without memory is re-rolling, not
  self-improving.
- **Determinism is an implementation's promise.** The Executor contract
  does not require a frozen environment; reference implementations
  provide one. See [port-executor.md](port-executor.md).
- **Degrade gracefully.** Optional ports (KnowledgeBase) may report
  `{"unavailable": true}`; the engine continues with reduced capability.
