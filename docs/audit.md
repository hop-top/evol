# The run ledger

Every loop run leaves one audit entry: what ran, against which
artifact, how it ended, and one step per generation. `evol runs list`
and `evol runs show <run-id>` read the ledger back — the operator's
answer to "what has the loop been doing, and why did that promotion
happen?"

## Where the ledger lives

Auditing crosses the [Audit port](../spec/port-audit.md), so the
ledger's home is a configuration choice, not a hard dependency — the
same rule every other exchange in the loop follows:

- **[`audit-tlc`](../adapters/audit-tlc/README.md)** — the family
  arrangement: run records land in the task tracker's external-run
  audit surface, project-scoped alongside task and flow history. One
  tracker becomes the audit home for every tool that adopts the same
  surface; the loop is just its first tenant.
- **[`audit-fs`](../adapters/audit-fs/README.md)** — a JSONL file.
  Zero dependencies, good enough for a laptop or CI.

```yaml
ports:
  audit:
    cmd: ["audit-tlc"]        # or ["audit-fs"] with EVOL_AUDIT_ROOT
    timeout_seconds: 30
```

## Semantics worth knowing

- **Audit never fails a run.** Unconfigured → the run notes once that
  auditing is disabled. A configured adapter failing at record time →
  one warning, the run's outcome stands. Reads (`evol runs …`) surface
  errors normally — exit 3 with an adapter hint when unconfigured.
- **Every outcome is recorded** — `promoted`, `no-improvement`,
  `gate-failed`, `error`. Negative results are ledger entries too;
  that is the point of a ledger.
- **Run ids are derived** from the start instant and the subject, so
  re-recording the same run upserts instead of duplicating.
- The record carries headline metrics (`baseline_score`, `best_score`,
  `sig_p` when significance ran, `generations`, `candidates_tried`);
  the full per-candidate evidence stays in the
  [corpus](../spec/port-corpus.md) — the ledger is the index, not the
  archive.
