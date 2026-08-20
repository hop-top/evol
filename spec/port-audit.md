# Port: Audit

> Part of the [evol port contracts](README.md) — **DRAFT**, `evol: "1"`.
> New port introduced after the v1 publication; it stays draft until its
> stability review (see [publishing.md](publishing.md)) and, per the
> standing rule, ships no conformance fixtures until a second real
> adapter exists.

The operator-facing run ledger. The engine records one entry per loop
run — outcome, headline metrics, one step per generation — and the
`evol runs` commands read the ledger back. The port exists so the
ledger's *home* is swappable: the reference adapters store runs in the
family task tracker (`audit-tlc`) or a plain JSONL file (`audit-fs`);
any tool with run history can implement the same three actions.

The port is **optional**: with no `ports.audit` configured the engine
runs normally and notes once per run that auditing is disabled. A
configured adapter that fails at record time degrades the same way —
audit must never fail a run.

## The run record

| Field | Type | Notes |
|-------|------|-------|
| `tool` | string | always `"evol"` from this engine; the ledger is tool-agnostic |
| `run_id` | string | unique per run; upsert key together with `tool` |
| `subject` | string | what the run was about — the artifact ref |
| `started_at` | string | RFC3339 |
| `finished_at` | string | RFC3339 |
| `outcome` | string | `promoted` \| `no-improvement` \| `gate-failed` \| `error` |
| `metrics` | object | flat map of numbers: `baseline_score`, `best_score`, `generations`, `candidates_tried`, `sig_p` when present |
| `steps` | array | one per phase, ordered by `seq` |
| `steps[].seq` | number | 0 = baseline, then one per generation |
| `steps[].name` | string | `baseline`, `generation-1`, … |
| `steps[].status` | string | `ok` \| `accepted` \| `explored` \| `dry` |
| `steps[].detail` | string? | one-line human summary |

## Actions

### `record`

Upserts one run by (`tool`, `run_id`). Re-recording the same id
replaces the entry.

```json
{"evol": "1", "port": "audit", "action": "record",
 "run": {"tool": "evol", "run_id": "run-20260820T120000Z-1a2b3c4d",
         "subject": "commit-messages/SKILL.md",
         "started_at": "2026-08-20T12:00:00Z",
         "finished_at": "2026-08-20T12:09:41Z",
         "outcome": "promoted",
         "metrics": {"baseline_score": 0.7049, "best_score": 0.8236,
                     "sig_p": 0.0002, "generations": 1,
                     "candidates_tried": 3},
         "steps": [
           {"seq": 0, "name": "baseline", "status": "ok",
            "detail": "0.7049 over 8 case(s) × 3 trial(s)"},
           {"seq": 1, "name": "generation-1", "status": "accepted",
            "detail": "3 candidate(s); best 0.8236 (cand-01, tighten)"}]}}
```

```json
{"evol": "1", "port": "audit", "action": "record"}
```

### `list`

Newest first (by `started_at`, ties by `run_id`).

Request: `tool?` (filter), `subject?` (exact match), `limit?` (number).

```json
{"evol": "1", "port": "audit", "action": "list",
 "tool": "evol", "subject": "commit-messages/SKILL.md", "limit": 20}
```

```json
{"evol": "1", "port": "audit", "action": "list", "runs": [{"…": "…"}]}
```

### `show`

Request: `run_id` (required), `tool?`. The full record, steps included.
An unknown `run_id` is an **adapter error** (non-zero exit) — the
ledger cannot distinguish "never recorded" from a typo, so it refuses
rather than serving an empty shell.

```json
{"evol": "1", "port": "audit", "action": "show",
 "tool": "evol", "run_id": "run-20260820T120000Z-1a2b3c4d"}
```

```json
{"evol": "1", "port": "audit", "action": "show", "run": {"…": "…"}}
```

## Error semantics

Single failure plane: adapter errors are non-zero exit with stderr
diagnostics. There is no `unavailable` response — the *engine* owns
degradation (skip + note) because auditing is optional by
configuration, not by environment.

## Reference adapters

- [`audit-tlc`](../adapters/audit-tlc/README.md) — the family ledger:
  records into the task tracker's external-run audit surface, so loop
  runs sit next to task and flow history.
- [`audit-fs`](../adapters/audit-fs/README.md) — zero-dependency JSONL
  ledger; the fallback when no tracker is around.
