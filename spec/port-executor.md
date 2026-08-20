# Port: Executor

> Part of the [evol port contracts](README.md) — published, `evol: "1"`.

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
| `env.provider` | string | optional model/provider URI for the agent under test (e.g. `claude://haiku`, `ollama://llama3.2:3b?base_url=http://127.0.0.1:11500`). Executors MUST expose it to the child verbatim (reference implementation: env var `EVOL_PROVIDER`); interpretation belongs to the run wrapper. |

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

## Reference runner contract

Executors that spawn a command to run the agent under
test SHOULD spawn one conforming to this contract, making the agent
runner a user-swappable seam with the same philosophy as the ports —
no tool is privileged:

| Channel | Meaning |
|---------|---------|
| stdin | the case input, verbatim |
| env `EVOL_CANDIDATE_REF` | path to the staged candidate artifact body |
| env `EVOL_PROVIDER` | optional provider/model URI; interpretation is the runner's — a runner handed a scheme it does not speak fails fast rather than running the wrong model |
| stdout | the agent's output ONLY — no banners, logs, or wrappers |
| non-zero exit | run failure (becomes `error`, scored as failed) |

The reference executor exposes `EVOL_CANDIDATE_REF` and
`EVOL_PROVIDER` on every child. Per-tool shims adapting this contract
to concrete CLIs live with the consuming project (e.g. `e2e/bin/runners/`).

## Notes

- `env.mode: "record"` runs should be reserved for baselines; recording
  during candidate evaluation makes candidates observe different worlds.
- Adapters that cannot observe tool calls return `"tool_calls": []` —
  scoring dimensions that need them simply get no signal.
- **Two recording layers exist in practice; know which one you are
  configuring.** Executor-level environment freezing (this port's
  `env.mode`, with adapter-scoped cassette locations) and runner-level
  recording wrappers (which wrap the spawned runner and key recordings
  on the candidate's *content* hash) are independent. When both are
  available, prefer content-hash identity for anything that must
  survive re-staging: staging paths churn between runs, content does
  not. An executor that derives cassette locations from the staged
  `candidate_ref` string should document that those locations are
  run-local.
