# gate-ben

Gate adapter over the [ben](https://github.com/hop-top/ben) benchmark
runner. ben measures and ranks but has no baseline, threshold, or
fail-on-regression concept — it exits 0 regardless of outcome. This
adapter supplies the missing verdict: run the suite, fetch the pinned
baseline run, compare per metric, pass or fail.

> **DRAFT CONTRACT.** The Gate port is Tier 2 and deliberately absent
> from `spec/` — Tier 2 contracts are extracted from working
> implementations, not designed up front. The request/response below is
> this adapter's proposal; it graduates to `spec/port-gate.md` once a
> second gate implementation exists.

## Contract (draft)

Request (stdin, one JSON object):

| Field | Type | Notes |
|-------|------|-------|
| `evol` | string | `"1"` |
| `port` | string | `"gate"` |
| `action` | string | `"check"` |
| `candidate_ref` | string | matched against ben candidate names; see matching below |
| `baseline` | object? | `{run_id}`; omit/null on the first run |
| `suite` | string | ben suite name |
| `metrics` | object[] | `{name, direction: "min"\|"max", threshold_delta >= 0}` |

Response (stdout, one JSON object):

| Field | Type | Notes |
|-------|------|-------|
| `pass` | bool | |
| `deltas` | object[] | `{metric, baseline, candidate, delta}` (candidate − baseline) |
| `run_id` | string | the candidate run; pin it as the next baseline on accept |
| `reason` | string | human-readable verdict |

Non-zero exit = adapter error (ben missing, ben failed, malformed
request, ambiguous candidate). **A gate that cannot run must not pass.**

## Semantics

- `ben run <suite> --format json` produces the candidate run; with a
  baseline, `ben show <run_id> --format json` fetches the comparison
  point. ben's `_meta` envelope (and a nested `data` payload, if
  present) is unwrapped defensively.
- Direction `min`: lower is better; regression when
  `candidate − baseline > threshold_delta`. Direction `max`: higher is
  better; regression when `baseline − candidate > threshold_delta`.
- A metric missing from either run **fails the gate** with an explicit
  reason. Silently skipping a metric is how regressions hide.
- No baseline → pass with reason `no baseline (first run)`; the engine
  pins the returned `run_id`.
- Candidate matching: exact `candidate_ref` == ben candidate name; else
  the sole candidate when the run has exactly one; anything else is an
  adapter error (ambiguous configuration).

## Environment

| Var | Default | Purpose |
|-----|---------|---------|
| `EVOL_BEN_BIN` | `ben` | ben binary (tests point this at a fake) |
| `EVOL_BEN_TIMEOUT` | `300s` | whole-check deadline (Go duration) |

## Example

```sh
echo '{"evol":"1","port":"gate","action":"check",
  "candidate_ref":"cand-a","baseline":{"run_id":"01J..."},
  "suite":"core",
  "metrics":[{"name":"latency_ms","direction":"min","threshold_delta":5},
             {"name":"accuracy","direction":"max","threshold_delta":0.02}]}' \
  | gate-ben
```

## Deliberately not implemented (v0)

- Repetitions / statistical significance — single ben run per check;
  belongs upstream in ben or in the engine's trials loop.
- Auto-pinning baselines — the engine owns baseline lifecycle.
- Suite mutation or generation — the suite is operator-authored.
