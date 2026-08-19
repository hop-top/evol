# executor-apx

Executor-port adapter (see `spec/port-executor.md`): runs one candidate
against one eval case as a subprocess, in up to three layers. Each layer
is opt-in via environment; with none of the optional layers configured
this is the "plain subprocess, valid but weaker" implementation the spec
names — no frozen environment, no isolation.

## Layers

| Layer | Enabled by | Effect |
|-------|-----------|--------|
| plain | always | run `EVOL_EXEC_CMD` template, capture stdout/exit/duration |
| +xrr | `EVOL_XRR_MODE` | inject `XRR_MODE` + `XRR_CASSETTE_DIR` into the child so xrr-aware seams (shims, HTTP clients) record/replay |
| +aps | `EVOL_APS_PROFILE` | wrap the child in `aps run <profile> --env K=V… -- <cmd>` for per-candidate identity/isolation |

## Environment

| Var | Required | Meaning |
|-----|----------|---------|
| `EVOL_EXEC_CMD` | yes | JSON argv array; `{input}` and `{candidate_ref}` placeholders substituted per element |
| `EVOL_EXEC_TIMEOUT` | no | Go duration (`90s`) or integer seconds; default 120s; expiry = run failure, not adapter failure |
| `EVOL_XRR_MODE` | no | enables the xrr layer; default mode when the request's `env.mode` is empty |
| `EVOL_XRR_CASSETTE_DIR` | no | explicit cassette dir |
| `EVOL_XRR_CASSETTE_ROOT` | with layer | fallback: per-candidate subdir `<root>/<sha256(candidate_ref)[:12]>/` |
| `EVOL_APS_PROFILE` | no | enables aps wrapping |
| `EVOL_APS_BIN` | no | aps binary override (tests) |

Mode resolution: the request's `env.mode` wins over `EVOL_XRR_MODE`
(the engine varies record-for-baseline / replay-for-candidate per run);
`live` skips injection entirely. Requesting `replay`/`record` while the
xrr layer is disabled is an adapter failure — the engine asked for a
frozen world this configuration cannot provide.

## Error planes

- Adapter failure (non-zero exit, stderr diagnostics): bad request,
  bad config, unstartable child, frozen mode without the xrr layer.
- Run failure (exit 0, `error` set): timeout, child killed by signal.
- Non-zero child exit is **data**: `transcript.exit_code` carries it,
  `error` stays empty.

## Determinism caveat (design note)

xrr freezes the **tool/environment side only**. Candidates change the
prompt by construction, so recorded LLM responses never replay across
candidates (prompt bytes are in the cassette fingerprint). The working
pattern is a two-session split: tool calls replay from the baseline
cassette (PATH shims opt in per binary), while LLM traffic runs live
(or records) per candidate. v0 of this adapter injects ONE mode for the
whole child — an xrr limitation (one mode per session); the split lives
in which seams honor the env (shims for tools) versus which are pointed
at a separate session by the agent under test. Un-shimmed binaries
silently escape determinism; prefer a catchall shim plus an allowlist
when coverage matters.

## Deliberately not implemented (v0)

- `transcript.tool_calls` is always `[]` — populating it from cassette
  post-processing is a later iteration.
- No local recording proxy for non-instrumentable agent CLIs (base-URL
  override seams).
- aps stdout envelope stripping is defensive (bracket-prefixed
  `[exec]`/`[exit]` lines and JSONL objects marked exec/exit); the exact
  envelope schema is not pinned by aps docs yet.

## Example

```sh
export EVOL_EXEC_CMD='["my-agent","--prompt","{input}"]'
export EVOL_XRR_MODE=replay
export EVOL_XRR_CASSETTE_ROOT=/tmp/evol-cassettes
export EVOL_APS_PROFILE=cand0
echo '{"evol":"1","port":"executor","action":"run",
      "candidate_ref":"staging/cand-01",
      "case":{"id":"case-017","input":"Write a commit message"},
      "env":{"mode":"replay"}}' | executor-apx
```
