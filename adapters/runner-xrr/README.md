# runner-xrr

Cassette record/replay wrapper for any runner conforming to the
reference runner contract (`spec/port-executor.md`). Repeated
(candidate content, case input, provider) triples replay from disk
instead of re-spending agent calls.

```
runner-xrr <real-runner> [args...]
```

Wraps the real runner transparently: same stdin, same env
(`EVOL_CANDIDATE_REF`, `EVOL_PROVIDER`), stdout and exit code
propagated. Uses `hop.top/xrr` (exec adapter) for storage.

## Fingerprint identity

The cassette key deliberately does NOT use the real argv or the staged
candidate path — both carry per-checkout absolute paths and per-run
temp names. Instead the shim hands xrr a synthetic identity:

```
argv  = [basename(real-runner), "candidate:" + sha256(candidate file content)[:12],
         "provider:" + $EVOL_PROVIDER]
stdin = case input
```

Candidate CONTENT is the identity: the same body staged at a different
temp path replays; a one-byte body change records fresh. Distinct
providers record separately. No environment values are persisted into
cassettes — keep credentials in runner env vars, never in
`EVOL_PROVIDER` URIs.

## Modes (`XRR_MODE` / `XRR_CASSETTE_DIR`)

| Mode | Behavior |
|------|----------|
| `record` | spawn real runner, persist (stdout, stderr, exit, duration), return them verbatim |
| `replay` | serve recorded stdout/stderr/exit; real runner is NOT spawned |
| `passthrough` / unset | spawn without touching cassettes |

Exit codes added by the shim (both are "run failure" to the executor,
but distinguishable by operators):

| Exit | Meaning |
|------|---------|
| 20 | shim misconfiguration, spawn failure, or a replayed spawn-failure recording |
| 21 | replay-mode cassette miss — record this pair first |

## Caveats

- Record mode re-records identical pairs (timestamp churn in the
  cassette files; content is unchanged). Fine for evolution runs.
- New candidates ALWAYS miss in replay — their content has never been
  recorded. Use `record` during evolution runs (cache-as-you-go),
  `replay` for regression re-runs of already-promoted artifacts.
- The child inherits `XRR_MODE`/`XRR_CASSETTE_DIR`; an xrr-aware tool
  further down would record into the same directory (harmless for the
  shipped shims, which are not xrr-aware).

## Wiring

`e2e/evol.yaml` keeps `executor: [e2e/bin/executor-apx]`; only the
command the executor spawns changes:

```sh
go build -buildvcs=false -o e2e/bin/runner-xrr ./adapters/runner-xrr
export EVOL_EXEC_CMD='["e2e/bin/runner-xrr","e2e/bin/runners/claude.sh"]'
export XRR_MODE=record
export XRR_CASSETTE_DIR=e2e/cassettes
```

See `e2e/RUNBOOK.md` "Recording runs (cassettes)".
