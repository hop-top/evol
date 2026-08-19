# scorer-eva

Scores a candidate transcript by piping it through the
[eva](https://github.com/hop-top/eva) CLI's standalone contract mode.

> **Draft port.** `scorer` is not yet part of `spec/` — Tier-2 contracts
> are extracted from the working implementation, not designed up front.
> The shapes below are the reference that extraction will start from.

## Contract (draft)

Request (stdin):

```json
{"evol": "1", "port": "scorer", "action": "score",
 "case": {"id": "case-017", "input": "...", "expected_output": "..."},
 "transcript": {"output": "..."}}
```

Response (stdout):

```json
{"evol": "1", "port": "scorer", "action": "score",
 "score": {"value": 0.6, "reason": "word_count: too short; contains: found phrase"}}
```

- `value` — mean of evaluator scores from the eva report, clamped to
  [0, 1]. When no evaluator carries a score, falls back to the report's
  `passed` flag (1.0 / 0.0).
- `reason` — evaluator reasons joined `; `, failing evaluators first,
  skipped evaluators appended. Capped at 1000 chars.

## How it invokes eva

```
eva run --contract $EVOL_EVA_CONTRACT --input - --format json
```

The transcript output is piped on stdin. Exit-code handling:

| eva exit | Meaning | Adapter behavior |
|----------|---------|------------------|
| 0 | contract passed | parse report (stdout first) |
| 1 | evaluation failure | parse report (stderr first — eva prints the JSON report to stderr on failure) |
| 2 | bad input / invalid contract | adapter error (non-zero exit) |

Both streams are always tried — the stdout/stderr split is an eva quirk,
not a contract.

## Limitations (current eva)

**Keep contracts to programmatic evaluators** (`contains`, `regex`,
`equals`, `json_schema_valid`, `word_count`, `exit_code`, …). LLM-judge
evaluators are not wired to an LLM client in eva's standalone mode yet;
a single judge reference in the contract makes the whole run exit 2 —
which this adapter surfaces as an adapter error. Revisit once eva ships
judge wiring; the port contract does not change.

## Environment

| Var | Required | Default | Purpose |
|-----|----------|---------|---------|
| `EVOL_EVA_CONTRACT` | yes | — | path to the eva contract YAML |
| `EVOL_EVA_BIN` | no | `eva` | eva binary (overridden in tests) |
| `EVOL_EVA_TIMEOUT` | no | `60s` | per-invocation timeout (Go duration) |

## Example

```sh
export EVOL_EVA_CONTRACT=contracts/commit-style.yaml
echo '{"evol":"1","port":"scorer","action":"score",
      "case":{"id":"c1","input":"..."},
      "transcript":{"output":"feat(api): add rate limits"}}' | scorer-eva
```
