# audit-tlc

Audit port adapter ([spec](../../spec/port-audit.md)) that makes the
family task tracker the ledger's home: loop runs land in `tlc`'s
external-run audit surface, next to task and flow history, scoped to
the same project.

## Verb mapping

| Port action | tlc invocation |
|-------------|----------------|
| `record` | `tlc audit record --tool <tool> --stdin` (run JSON on stdin) |
| `list` | `tlc audit list --format json [--tool t] [--subject s] [--limit N]` |
| `show` | `tlc audit show <run-id> --format json [--tool t]` |

Output parsing tolerates both a bare JSON array and a `{"runs": [...]}`
wrapper on `list`, and unwraps a `{"run": {...}}` envelope on `show` —
additive drift on the tracker side must not break the consumer.

## Configuration

| Env | Meaning |
|-----|---------|
| `EVOL_TLC_BIN` | tracker binary (default `tlc`) |
| `EVOL_TLC_CHDIR` | optional; passed as the global `-C <dir>` so runs land in an explicit project instead of the cwd-resolved one |
| `EVOL_TLC_TIMEOUT` | per-call deadline (Go duration, default `30s`) |

## Error semantics

Tracker missing, non-zero exits, timeouts, unparseable output — all
adapter errors (non-zero exit). The engine treats a failing `record`
as degradation (a run must never fail because its ledger did); the
`evol runs` commands surface list/show errors directly.

## Project scoping

None by default: the adapter runs in the engine's working directory and
the tracker resolves the project from there, exactly like invoking it
by hand. Set `EVOL_TLC_CHDIR` to pin a project explicitly.
