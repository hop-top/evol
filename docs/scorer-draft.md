# Scorer — engine-level draft contract

> **STATUS: ENGINE DRAFT.** The port contracts in [spec/](../spec/)
> deliberately defer Tier-2 ports (Scorer/Judge, Gate, Reviewer, Audit)
> until they can be extracted from a working implementation. The engine
> still needs a scoring seam today; this document pins the shape it
> speaks. When the Scorer port graduates into `spec/`, this file is
> superseded.

Same wire protocol as every port (JSON over stdio, envelope
`{"evol": "1", "port": "scorer", "action": ...}`, non-zero exit =
adapter error).

## Action: `score`

Score one transcript against one eval case. Value is normalized to
`0..1`, higher is better. `reason` is free text and is recorded to the
corpus verbatim — write it for the humans reviewing verdicts later.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `case.id` | string | eval case id |
| `case.input` | string | the task input that was run |
| `case.expected` | string | expected behavior, rubric prose |
| `transcript` | object | as returned by the Executor port `run` |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `score.value` | number | `0..1`, higher is better |
| `score.reason` | string | why — recorded to the corpus |

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

## Engine behavior

- The engine calls `score` once per case per trial, for the baseline
  and for every candidate; means are compared at the gate.
- A run-level Executor failure never reaches the scorer: the engine
  records score `0.0` with the run error as the reason.
- Reference implementations shell out to an evaluation framework
  (deterministic checks + LLM judges); a keyword or exact-match scorer
  is a valid, weaker adapter.
