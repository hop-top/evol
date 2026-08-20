# evol port contracts

> **STATUS: INTERNAL DRAFT** — contracts are unstable until a first
> end-to-end improvement is demonstrated. Field names, envelope shape,
> and action sets may change without notice. No conformance fixtures
> ship until a second real adapter exists for any given port. The path
> from draft to published is [publishing.md](publishing.md); the fixture
> design is [conformance-plan.md](conformance-plan.md).

evol is a self-improvement loop for agent capabilities: it evaluates an
artifact (a skill, prompt, command, or tool config), proposes candidate
revisions, executes them against eval cases, scores the results, gates
them against a baseline, and promotes or rejects — writing every verdict
back to a corpus so the next generation starts smarter.

The loop engine owns control flow only. Every I/O exchange crosses a
**port**: a versioned JSON contract spoken over process boundaries.
Implementations are **adapters** — standalone executables in any
language. The engine never links adapter code, and an adapter needs no
SDK: if it can read one JSON object and write one back, it qualifies.

## Wire protocol

One invocation per action:

1. The caller spawns the adapter executable (argv is caller
   configuration, opaque to this spec).
2. The caller writes exactly one JSON request object to the adapter's
   stdin and closes it.
3. The adapter writes exactly one JSON response object to stdout and
   exits.

Streams and exit codes:

| Channel | Rule |
|---------|------|
| stdin | one request object, then EOF |
| stdout | one response object, nothing else — no banners, logs, or progress |
| stderr | free-form diagnostics; never parsed, always safe to write |
| exit 0 | response on stdout is valid and complete |
| exit non-zero | **adapter error** — stdout is ignored, stderr explains |

Some ports layer a second failure plane on top of exit codes (e.g. the
Executor's *run failure*, which is data, not an error); each port file
defines its own planes explicitly.

Timeouts are the caller's job: the reference engine bounds every
invocation (60 s default, configurable per port) and kills the adapter
process group on deadline. Adapters should not implement their own
outer timeout; long-running internals (an LLM call, a slow store)
should carry their own tighter budgets so the adapter can fail with a
useful stderr message rather than being killed silently.

Every request and response envelope carries:

| Field | Type | Notes |
|-------|------|-------|
| `evol` | string | contract version; always `"1"` for this draft |
| `port` | string | port name, e.g. `"generator"` |
| `action` | string | action name, e.g. `"propose"` |

Request payload fields sit alongside the envelope fields; response
payloads likewise. All field names are `snake_case`. Adapters must
ignore unknown fields (additive evolution depends on it) and must not
require fields a port file marks optional.

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
| KnowledgeBase | contextual knowledge grounding proposals; optional | 1 | [port-knowledgebase.md](port-knowledgebase.md) |

Tier 2 ports (Scorer/Judge, Gate, Reviewer, Audit) are deliberately not
specified yet: they will be extracted from the working reference
implementation, not designed up front.

One level below the Executor sits the **reference runner contract** — a
four-line stdin/env/stdout convention that makes the agent under test a
user-swappable seam (any agent CLI or LLM pipe becomes a ~15-line
shim). It lives in [port-executor.md](port-executor.md), not here,
because it binds runner processes, not port adapters.

## Versioning & stability

- The envelope's `evol` field names the contract version. It stays
  `"1"` throughout the draft period and after first publication;
  it changes only on a breaking change.
- **Breaking** means: removing or renaming a field or action, changing
  a field's type or meaning, tightening a requirement on adapters, or
  changing exit-code semantics. Any of these bumps the version.
- **Additive** means: new optional request/response fields, new
  actions, new enum values callers already tolerate. Additive changes
  do not bump the version — which is why adapters must ignore unknown
  fields.
- During the draft period, additions are annotated *"added while
  INTERNAL DRAFT"* in the port files. At publication those annotations
  are folded into the base tables and disappear; from then on the
  additive/breaking rules above are a commitment, not an intention.
- Adapters that predate an optional action may answer it with an
  adapter error; callers are expected to degrade (the Corpus `history`
  action documents this pattern).

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
