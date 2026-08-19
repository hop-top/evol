# corpus-fs

File-backed implementation of the [Corpus port](../../spec/port-corpus.md):
eval cases, candidate verdicts, tabu history, and human corrections as
JSONL under a plain directory. Stdlib-only by design — no database
driver. An implementation backed by an evaluation store's SQLite replaces
this behind the same port once its machine-readable dataset mode lands.

## Layout

| Path (under `$EVOL_CORPUS_ROOT/<sha256(artifact_ref)[:12]>/`) | Contents | Written by |
|---|---|---|
| `ref.txt` | the artifact_ref, for humans browsing the store | first `record` |
| `cases.jsonl` | eval cases `{id, input, expected, split, source}` | seeded externally |
| `generations.jsonl` | one line per recorded candidate, generation info embedded | `record` (append-only) |
| `corrections.jsonl` | corrections in case shape | external review tooling |

## Actions

- `cases` — reads `cases.jsonl`, filters by `split` when given, dedups by
  content hash of `input` + `expected` (first occurrence wins).
- `record` — appends every candidate as one JSONL line via a single
  `O_APPEND` write; creates the artifact directory on first use.
  Timestamps are only stored when the request carries them.
- `tabu` — every recorded candidate whose verdict is not `accepted`
  (i.e. `rejected` and `failed`), distilled to
  `{strategy, rationale, verdict}` and deduped on (strategy, rationale) —
  cheap by construction, per the port note.
- `corrections` — reads `corrections.jsonl`; `source` is forced to
  `correction` when absent.

Reads on artifacts never recorded return empty results (exit 0).
Malformed JSONL lines are skipped with a stderr warning, never fatal.

## Environment

| Var | Required | Purpose |
|-----|----------|---------|
| `EVOL_CORPUS_ROOT` | yes | root directory of the store |

## Example

```sh
export EVOL_CORPUS_ROOT=.evol/corpus
echo '{"evol":"1","port":"corpus","action":"tabu",
      "artifact_ref":"skills/commit-style"}' | corpus-fs
```
