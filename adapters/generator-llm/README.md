# generator-llm

Generator port adapter: proposes candidate revisions of an artifact by
applying LLM mutation strategies, one Anthropic Messages API call per
candidate. Contract: [spec/port-generator.md](../../spec/port-generator.md).

## Environment

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `ANTHROPIC_API_KEY` | yes | — | API auth; missing → non-zero exit |
| `EVOL_GENERATOR_MODEL` | no | `claude-sonnet-5` | Messages API model id |
| `ANTHROPIC_BASE_URL` | no | `https://api.anthropic.com` | override for tests and recording proxies |

## Strategies (v0)

Applied round-robin, one per candidate, up to `budget.max_candidates`:

| Strategy | Mutation |
|----------|----------|
| `tighten` | remove redundancy, keep every distinct behavior |
| `restructure` | reorder sections to follow the reader's task flow |
| `add-example` | add one concrete worked example the feedback asks for |
| `sharpen-triggers` | make when-to-use conditions explicit and testable |

The prompt carries scoring history, knowledge passages, and the tabu
list with an explicit instruction not to re-propose tabu'd approaches.
Constraints pinned in the system prompt: preserve all frontmatter
fields, stay within +20% of baseline size, return a complete artifact.

## Invocation

```sh
echo '{"evol":"1","port":"generator","action":"propose",
  "artifact":{"ref":"skills/commit-style","kind":"skill",
              "frontmatter":"name: commit-style","body":"...","version":"b1946ac9"},
  "scores":[],"tabu":[],"budget":{"max_candidates":2}}' \
  | ANTHROPIC_API_KEY=... generator-llm
```

## Error semantics

- Missing API key, malformed request, wrong envelope, HTTP 401/403:
  adapter error — non-zero exit, diagnostics on stderr.
- Per-candidate failures (transport error, HTTP 5xx, unparseable model
  output): candidate dropped with a stderr diagnostic; remaining
  candidates still returned. Zero surviving candidates is a valid dry
  generation (exit 0, `"candidates": []`).

## Cost

One Messages API call per requested candidate (`max_tokens` 4096,
60s timeout, no retries in v0).

## Deliberately not implemented (v0)

- Retries/backoff; streaming; response caching.
- Strategy selection informed by tabu statistics (currently plain
  round-robin; the tabu list only steers the prompt).
- Non-Anthropic providers.
