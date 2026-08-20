# Port: Generator

> Part of the [evol port contracts](README.md) — INTERNAL DRAFT, `evol: "1"`.

Proposes candidate revisions of an artifact. The generator is the
mutation half of the loop; everything else measures. Implementations
range from LLM mutation strategies to full prompt optimizers — the
engine cannot tell the difference, and must not need to.

The engine supplies scoring history and tabu entries so a generator can
avoid re-proposing what already lost. Consuming them is strongly
recommended, not enforced.

## Actions

### `propose`

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact` | object | as returned by [ArtifactStore](port-artifactstore.md) `load`: `{ref, kind, frontmatter, body, version}` |
| `scores` | object[] | recent scoring summaries for this artifact: `{version, score, feedback?}` |
| `tabu` | object[] | past rejects: `{strategy, rationale, verdict}` — do not re-propose these |
| `knowledge` | object[]? | optional passages from the [KnowledgeBase](port-knowledgebase.md): `{text, source}` |
| `budget.max_candidates` | int | upper bound on candidates returned |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `candidates` | object[] | proposed revisions, at most `max_candidates` |
| `candidates[].id` | string | generator-unique candidate id |
| `candidates[].frontmatter` | string | full revised frontmatter |
| `candidates[].body` | string | full revised body |
| `candidates[].rationale` | string | why this revision should score better |
| `candidates[].strategy` | string | free-form strategy label, e.g. `tighten`, `add-example`, `reorder` |
| `candidates[].provider` | string | optional — resolved provider URI that produced this candidate, secrets stripped (added while INTERNAL DRAFT; consumed by model-comparison work) |

```json
{"evol": "1", "port": "generator", "action": "propose",
 "artifact": {"ref": "skills/commit-style", "kind": "skill",
              "frontmatter": "name: commit-style\n...",
              "body": "## When to use\n...", "version": "b1946ac9"},
 "scores": [{"version": "b1946ac9", "score": 0.71,
             "feedback": "misses scoped-package examples"}],
 "tabu": [{"strategy": "reorder", "rationale": "moved examples first",
           "verdict": "rejected: holdout regression"}],
 "knowledge": [{"text": "Conventional Commits requires a type prefix...",
                "source": "notes/conventional-commits"}],
 "budget": {"max_candidates": 3}}
```

```json
{"evol": "1", "port": "generator", "action": "propose",
 "candidates": [
   {"id": "cand-01", "strategy": "add-example",
    "frontmatter": "name: commit-style\n...",
    "body": "## When to use\n...\n## Examples\n...",
    "rationale": "feedback names missing scoped-package examples; adds two"}
 ]}
```

### `synth` (optional)

Generate NEW eval cases grounded in knowledge — distinct from `propose`
(which mutates the artifact). Optional: adapters without it answer with
an adapter error and callers report that cleanly. Cases produced this
way MUST be treated as unreviewed by callers — the engine quarantines
them via the [Corpus](port-corpus.md) `add-cases` action.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `artifact` | object | the artifact the cases will evaluate (same shape as `propose`) |
| `knowledge` | object[] | grounding passages `{text, source}` — synthesis without grounding invents circular evals from the artifact text; callers SHOULD refuse to synthesize with an empty list |
| `examples` | object[] | existing cases `{input, expected}` as style references |
| `count` | int | cases requested (adapters may return fewer) |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `cases` | object[] | `{input, expected?, rationale?}` — no ids, splits, or provenance; the caller assigns those |

```json
{"evol": "1", "port": "generator", "action": "synth",
 "artifact": {"ref": "skills/commit-style", "kind": "skill", "body": "..."},
 "knowledge": [{"text": "Dependency bumps use the build type.", "source": "notes/style"}],
 "examples": [{"input": "Commit message for a fix touching two packages",
               "expected": "type(scope) prefix; imperative subject"}],
 "count": 3}
```

```json
{"evol": "1", "port": "generator", "action": "synth",
 "cases": [{"input": "Commit message for bumping a lockfile-only dependency",
            "expected": "build: prefix; body optional",
            "rationale": "exercises the dependency-bump type rule"}]}
```

## Notes

- Candidates are complete artifacts, not diffs. The engine never merges.
- `strategy` values are free-form strings; the engine records them
  verbatim into tabu entries via the [Corpus](port-corpus.md) port, so
  stable naming across runs improves tabu usefulness.
- A generator may return fewer candidates than budgeted, or zero
  (`"candidates": []`) when it has nothing new to try — the engine
  treats an empty proposal as a dry generation, not an error.
