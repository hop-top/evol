# Port: Corpus

> Part of the [evol port contracts](README.md) — published, `evol: "1"`.

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

Quarantined cases (see `add-cases`) are NEVER served by `cases` — they
enter the eval pool only after `promote-cases` clears them.

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
| `candidates[].verdict` | string | `accepted`, `rejected`, `failed`, or `evidence` — evidence rows are provider-sweep observations (a candidate or the baseline scored under a non-primary provider); they inform routing decisions and MUST be excluded from tabu and history summaries |
| `candidates[].rationale` | string | why — gate result, judge feedback, constraint hit |
| `candidates[].strategy` | string? | generator strategy that produced the candidate; recorded so tabu keeps its strategy dimension (empty on baseline evidence rows) |
| `candidates[].provider` | string? | optional executor provider URI these scores were produced under; sweep rows always carry it |
| `candidates[].fixtures` | object? | optional `{cassette_dir}` — recorded-environment location pinned with a promoted run, for regression replay |
| `candidates[].recorded_at` | string? | RFC3339 wall-clock stamped by the engine at record time; the artifact's last-evolution clock for target selection. Additive post-publication per the [versioning rules](README.md#versioning--stability); nothing fingerprints or replays it |

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

### `history`

Per-generation outcome summary for an artifact, shaped for target
selection (`evol targets`, `evol run --select`). Adapters that predate
it may answer with an adapter error; callers degrade to "history
unknown".

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact_ref` | string | |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `generations` | object[] | `{generation, best_score, verdict, provider?, recorded_at?}` — one entry per recorded generation, ascending; `best_score` is the best candidate mean in that generation and `verdict` is that candidate's verdict; `recorded_at` is the latest stamp among the generation's non-evidence rows (absent for rows recorded before stamps existed) |

```json
{"evol": "1", "port": "corpus", "action": "history",
 "artifact_ref": "skills/commit-style"}
```

```json
{"evol": "1", "port": "corpus", "action": "history",
 "generations": [{"generation": 1, "best_score": 0.62, "verdict": "rejected"},
                 {"generation": 2, "best_score": 0.71, "verdict": "accepted"}]}
```

### `add-cases`

Add eval cases — the intake for synthesized and mined cases. Cases from
automated producers MUST arrive quarantined; only reviewed cases join
the eval pool (via `promote-cases`). Dedup is the adapter's job: a case
whose `(input, expected)` content already exists (quarantined or not)
counts as a duplicate and is not re-added.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact_ref` | string | |
| `cases` | object[] | `{id?, input, expected?, split, source, quarantined}` — `id` may be omitted; adapters then assign a deterministic content-derived id |
| `cases[].source` | string | provenance: `synthetic`, `mined`, … |
| `cases[].quarantined` | bool | quarantined cases are excluded from `cases` responses until promoted |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `added` | int | cases newly stored |
| `duplicates` | int | skipped as content duplicates |
| `ids` | string[] | ids of the added cases (review handle) |

```json
{"evol": "1", "port": "corpus", "action": "add-cases",
 "artifact_ref": "skills/commit-style",
 "cases": [{"input": "Commit message for a dependency bump",
            "expected": "build: prefix; no scope needed",
            "split": "train", "source": "synthetic", "quarantined": true}]}
```

```json
{"evol": "1", "port": "corpus", "action": "add-cases",
 "added": 1, "duplicates": 0, "ids": ["syn-9f2ab61c04d1"]}
```

### `promote-cases`

Clear quarantine on reviewed cases so they join the eval pool.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact_ref` | string | |
| `ids` | string[] | case ids to promote |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `promoted` | int | cases whose quarantine was cleared |
| `missing` | string[] | requested ids not found |

```json
{"evol": "1", "port": "corpus", "action": "promote-cases",
 "artifact_ref": "skills/commit-style", "ids": ["syn-9f2ab61c04d1"]}
```

```json
{"evol": "1", "port": "corpus", "action": "promote-cases",
 "promoted": 1, "missing": []}
```

## Notes

- `record` is append-only from the engine's view; dedup and versioning
  are adapter concerns.
- Adapters should make `tabu` cheap — the engine calls it every
  generation before `propose`.

See also: [Executor](port-executor.md) failures are recorded as
`verdict: "failed"`; [KnowledgeBase](port-knowledgebase.md) is the
unstructured complement to this structured store.
