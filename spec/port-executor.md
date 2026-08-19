# Port: Executor

> Part of the [evol port contracts](README.md) — INTERNAL DRAFT, `evol: "1"`.

Runs one candidate against one eval case and returns the transcript.
The executor is where the candidate meets the world: an agent session, a
CLI invocation, a sandboxed run — the contract does not care, only the
transcript shape does.

**Determinism is the implementation's promise, not the interface's.**
Reference implementations freeze the environment (recorded cassettes for
exec/HTTP/SQL and friends) and isolate each candidate (per-candidate
profile, sandbox, or container), so every candidate faces an identical
world and scores are comparable. A plain-subprocess implementation with
a live environment is a valid adapter — just a weaker one, and its
scores carry that caveat.

## Actions

### `run`

Request:

| Field | Type | Notes |
|-------|------|-------|
| `candidate_ref` | string | where the candidate artifact is staged (adapter-scoped; typically an ArtifactStore ref or a temp path the engine wrote) |
| `case.id` | string | eval case id, from [Corpus](port-corpus.md) `cases` |
| `case.input` | string | the task input to run |
| `env.mode` | string | execution mode hint: `replay` (frozen environment), `record` (capture a fresh environment), `live` (no freezing) — adapters implement the subset they support |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `transcript.output` | string | final output produced for the case |
| `transcript.tool_calls` | object[] | ordered tool invocations: `{tool, input, output}` — empty if the run makes none or the adapter cannot observe them |
| `transcript.duration_ms` | int | wall-clock duration |
| `transcript.exit_code` | int? | present when the run is a process |
| `error` | string? | run-level failure (candidate crashed, environment miss); the invocation itself still exits 0 |

```json
{"evol": "1", "port": "executor", "action": "run",
 "candidate_ref": "staging/cand-01",
 "case": {"id": "case-017", "input": "Write a commit message for a fix touching two packages"},
 "env": {"mode": "replay"}}
```

```json
{"evol": "1", "port": "executor", "action": "run",
 "transcript": {
   "output": "fix(parser,lexer): handle empty input without panic",
   "tool_calls": [{"tool": "git", "input": "diff --stat", "output": "2 files changed"}],
   "duration_ms": 8412,
   "exit_code": 0
 }}
```

## Error semantics

Distinguish two failure planes:

- **Adapter failure** — cannot stage the candidate, unsupported
  `env.mode`, broken configuration: exit non-zero, diagnostics on
  stderr. The engine aborts the generation.
- **Run failure** — the candidate itself crashed, timed out, or the
  frozen environment had no recording for a request it made: exit 0 with
  `error` set. The engine scores the case as failed and continues; the
  failure is data, recorded to the [Corpus](port-corpus.md) like any
  other verdict.

## Notes

- `env.mode: "record"` runs should be reserved for baselines; recording
  during candidate evaluation makes candidates observe different worlds.
- Adapters that cannot observe tool calls return `"tool_calls": []` —
  scoring dimensions that need them simply get no signal.
