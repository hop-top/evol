# Port: Scorer

> Part of the [evol port contracts](README.md) — published, `evol: "1"`.
> Tier 2: the first Tier-2 contract to graduate, extracted from the
> working engine seam rather than designed up front.

Scores one transcript against one eval case. Value is normalized to
`0..1`, higher is better. `reason` is free text and is recorded to the
[Corpus](port-corpus.md) verbatim — write it for the humans reviewing
verdicts later, and name the specific checks or criteria that failed:
generators consume these reasons as their only signal about *why* a
candidate lost.

## Actions

### `score`

Request:

| Field | Type | Notes |
|-------|------|-------|
| `case.id` | string | eval case id, from [Corpus](port-corpus.md) `cases` |
| `case.input` | string | the task input that was run |
| `case.expected` | string? | expected behavior, rubric prose — optional; scorers with their own rubric may ignore it |
| `transcript` | object | as returned by the [Executor](port-executor.md) `run` action |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `score.value` | number | `0..1`, higher is better |
| `score.reason` | string | why — recorded to the corpus verbatim |

```json
{"evol": "1", "port": "scorer", "action": "score",
 "case": {"id": "case-017",
          "input": "Write a commit message for a fix touching two packages",
          "expected": "type(scope) prefix; imperative subject; no period"},
 "transcript": {"output": "fix(parser,lexer): handle empty input",
                "tool_calls": [], "duration_ms": 8412, "exit_code": 0}}
```

```json
{"evol": "1", "port": "scorer", "action": "score",
 "score": {"value": 0.83, "reason": "prefix and mood correct; scope list matches"}}
```

## Error semantics

The Scorer has a single failure plane: **adapter failure**. A scorer
that cannot score — its backing evaluator is missing, its contract file
is absent, its judge times out — exits non-zero with diagnostics on
stderr. It must never emit a fabricated score; a gate fed by guesses is
worse than an aborted generation.

(Contrast with the [Executor](port-executor.md), whose *run failures*
are data: a failed run never reaches the scorer at all — the engine
records score `0.0` with the run error as the reason.)

## Engine behavior (informative)

- The engine calls `score` once per case per trial, for the baseline
  and for every candidate; per-case means feed the acceptance gate.
- Reference implementations: an evaluation-framework adapter
  (deterministic checks, with LLM-judge tiers as the framework gains
  them) and a self-contained programmatic checker. A keyword or
  exact-match scorer is a valid, weaker adapter.
- Scoring dimensions that need `transcript.tool_calls` get no signal
  when the executor could not observe them — score what is present.
