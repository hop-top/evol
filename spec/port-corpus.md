# Port: Corpus

> Part of the [evol port contracts](README.md) — INTERNAL DRAFT, `evol: "1"`.

The loop's memory: eval cases, candidate verdicts, tabu history, and
human corrections, keyed by artifact ref. **Write-back is mandatory
engine behavior** — after every generation the engine records every
candidate and its verdict here, accepted and rejected alike. A loop
without memory is re-rolling, not self-improving.

Reference implementations back onto an evaluation store's database;
a JSONL directory or a fine-tuning dataset store are equally valid.

## Actions

### `cases`

Eval cases for an artifact, optionally restricted to a split.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact_ref` | string | artifact under evolution |
| `split` | string? | `train`, `val`, or `holdout`; omit for all |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `cases` | object[] | `{id, input, expected, split, source}` |
| `cases[].source` | string | provenance: `golden`, `synthetic`, `mined`, `correction` |

```json
{"evol": "1", "port": "corpus", "action": "cases",
 "artifact_ref": "skills/commit-style", "split": "holdout"}
```

```json
{"evol": "1", "port": "corpus", "action": "cases",
 "cases": [{"id": "case-017",
            "input": "Write a commit message for a fix touching two packages",
            "expected": "type(scope) prefix; imperative subject; no period",
            "split": "holdout", "source": "golden"}]}
```

### `record`

Persist one generation's outcomes. Called once per generation, always.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `generation` | object | `{artifact_ref, baseline_version, number}` |
| `candidates` | object[] | one entry per candidate evaluated |
| `candidates[].id` | string | generator candidate id |
| `candidates[].scores` | object[] | per-case or aggregate: `{case_id?, score, reason?}` |
| `candidates[].verdict` | string | `accepted`, `rejected`, or `failed` |
| `candidates[].rationale` | string | why — gate result, judge feedback, constraint hit |
| `candidates[].fixtures` | object? | optional `{cassette_dir}` — recorded-environment location pinned with a promoted run, for regression replay (added while INTERNAL DRAFT) |

Response: empty object (envelope only).

```json
{"evol": "1", "port": "corpus", "action": "record",
 "generation": {"artifact_ref": "skills/commit-style",
                "baseline_version": "b1946ac9", "number": 4},
 "candidates": [{"id": "cand-01",
                 "scores": [{"case_id": "case-017", "score": 0.83,
                             "reason": "adds scoped example; matches expected form"}],
                 "verdict": "rejected",
                 "rationale": "holdout mean below baseline + delta"}]}
```

```json
{"evol": "1", "port": "corpus", "action": "record"}
```

### `tabu`

Reject history for an artifact, shaped for the
[Generator](port-generator.md)'s `tabu` input.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact_ref` | string | |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `entries` | object[] | `{strategy, rationale, verdict}` distilled from recorded rejects |

```json
{"evol": "1", "port": "corpus", "action": "tabu", "artifact_ref": "skills/commit-style"}
```

```json
{"evol": "1", "port": "corpus", "action": "tabu",
 "entries": [{"strategy": "reorder", "rationale": "moved examples first",
              "verdict": "rejected: holdout regression"}]}
```

### `corrections`

Human corrections promoted to eval cases (source `correction`).

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact_ref` | string | |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `cases` | object[] | same shape as `cases` response entries |

```json
{"evol": "1", "port": "corpus", "action": "corrections",
 "artifact_ref": "skills/commit-style"}
```

```json
{"evol": "1", "port": "corpus", "action": "corrections",
 "cases": [{"id": "corr-003", "input": "Commit message for a revert",
            "expected": "revert: prefix with original subject",
            "split": "train", "source": "correction"}]}
```

## Notes

- `record` is append-only from the engine's view; dedup and versioning
  are adapter concerns.
- Adapters should make `tabu` cheap — the engine calls it every
  generation before `propose`.

See also: [Executor](port-executor.md) failures are recorded as
`verdict: "failed"`; [KnowledgeBase](port-knowledgebase.md) is the
unstructured complement to this structured store.
