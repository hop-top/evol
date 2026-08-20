# audit-fs

Audit port adapter ([spec](../../spec/port-audit.md)) over a single
JSONL ledger — the zero-dependency fallback when no family tracker is
around.

## Configuration

| Env | Meaning |
|-----|---------|
| `EVOL_AUDIT_ROOT` | ledger directory (required); runs live in `runs.jsonl` |

## Behavior

- `record` upserts by (`tool`, `run_id`) with an atomic tmp+rename
  rewrite of the whole file — last write wins, no duplicates.
- `list` serves newest first (`started_at` desc, ties by `run_id`),
  filtered by `tool` / `subject`, truncated by `limit`.
- `show` returns the full record; an unknown `run_id` is an adapter
  error by contract.

## Example

```sh
export EVOL_AUDIT_ROOT=.evol/audit
echo '{"evol":"1","port":"audit","action":"list","tool":"evol"}' | audit-fs
```

## Deliberately not implemented

Concurrent writers (single-loop assumption, same as the corpus
adapter), retention/pruning, cross-file sharding. The family-tracker
adapter ([audit-tlc](../audit-tlc/README.md)) is where multi-tool
history aggregation belongs.
