# generator-llm

Generator port adapter: proposes candidate revisions of an artifact by
applying LLM mutation strategies. Provider-agnostic via
`hop.top/kit/go/ai/llm` — one URI selects cloud or local models.

## Invocation

One JSON request on stdin, one response on stdout. Non-zero exit =
adapter error (bad request, unsupported scheme, missing key, auth
failure); a candidate that fails or parses badly is dropped with a
stderr diagnostic and exit stays 0 (empty `candidates` = dry
generation). See `spec/port-generator.md`.

```sh
echo "$REQUEST_JSON" | EVOL_GENERATOR_PROVIDER="anthropic://claude-sonnet-5" generator-llm
```

## Provider URIs

`EVOL_GENERATOR_PROVIDER` — `scheme://[host:port/]model[?param=val]`.
Supported schemes (explicit allowlist):

| Scheme | Backend | Key required |
|--------|---------|--------------|
| `anthropic` | Anthropic Messages API | yes — `ANTHROPIC_API_KEY` |
| `openai` | OpenAI (or any OpenAI-compatible via `?base_url=`) | yes — `OPENAI_API_KEY` |
| `openrouter`, `xai`, `groq`, `together`, `fireworks`, `deepseek`, `mistral` | hosted OpenAI-compatible | yes — scheme env var or `LLM_API_KEY` |
| `ollama` | local Ollama | no |
| `routellm` | RouteLLM router (`routellm://router:threshold?base_url=…`) | server-dependent |

Key resolution: `?api_key=` URI param → provider-specific env var →
universal `LLM_API_KEY`. The kit config file (`llm.yaml`) is
intentionally not consulted — an invocation is fully described by its
environment.

### Local models quick start

```sh
# Ollama on the default port
EVOL_GENERATOR_PROVIDER="ollama://qwen3" generator-llm < request.json

# Ollama on a custom port
EVOL_GENERATOR_PROVIDER="ollama://localhost:11500/qwen3" generator-llm < request.json

# LM Studio / llama.cpp / vLLM (OpenAI-compatible endpoints)
EVOL_GENERATOR_PROVIDER="openai://my-model?base_url=http://localhost:1234/v1&api_key=local" generator-llm < request.json
```

### Deprecated

`EVOL_GENERATOR_MODEL=<model>` still works, maps to
`anthropic://<model>`, and prints a deprecation note on stderr.

## Mutation strategies

Round-robin over the budget (`budget.max_candidates`); strategies
repeat when the budget exceeds four. Names are stable — the engine
records them into tabu entries verbatim.

| Strategy | Mutation |
|----------|----------|
| `tighten` | remove redundancy, keep every distinct behavior |
| `restructure` | reorder sections along the reader's task flow |
| `add-example` | add one concrete worked example the scores suggest is missing |
| `sharpen-triggers` | make when-to-use conditions explicit and testable |

Prompts carry the artifact, recent scores with feedback, optional
knowledge passages, and the tabu list with an explicit do-not-repropose
instruction. Output is parsed from `===FRONTMATTER===` /
`===BODY===` / `===RATIONALE===` fenced sections; surrounding chatter
is tolerated, marker omission is not.

Each returned candidate carries `provider` — the resolved URI with
secrets stripped — for model-comparison consumers.

## Error semantics

| Condition | Behavior |
|-----------|----------|
| malformed request / wrong envelope / budget ≤ 0 | exit 1 |
| unsupported scheme / invalid URI / missing required key | exit 1 |
| auth failure (401/403) mid-run | exit 1 — retrying other candidates is pointless |
| per-candidate API error or unparseable output | candidate dropped, stderr diagnostic, exit 0 |
| all candidates dropped | `candidates: []`, exit 0 (dry generation) |

One completion call per candidate, 60s timeout each, no adapter-level
retries (provider SDKs may retry transient statuses). Cost note: a
budget of N = N completion calls.

## Not implemented (v0)

- Streaming; multi-turn refinement; temperature/param plumbing.
- `routellm` threshold tuning — pass it in the URI, the server decides.
- Registering additional kit/llm schemes (`gemini`/`google`, `triton`)
  — add to the factory map when needed.
