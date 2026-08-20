# casegen-llm

Implements the OPTIONAL Generator-port action `synth`
(spec/port-generator.md): generate NEW eval cases grounded in knowledge
passages. Distinct from `generator-llm`, which mutates the artifact —
this adapter manufactures the cases that evaluate it.

One JSON request on stdin, one response on stdout; non-zero exit =
adapter error, stderr = diagnostics.

## Grounding is mandatory

An empty `knowledge` list is refused (exit 1, "circular-eval guard"):
cases invented from the artifact text alone let the artifact grade its
own homework. Callers supply knowledgebase passages; the engine's
`evol cases synth` refuses before even calling when none are available.

Cases returned here are unreviewed by definition. The engine lands them
QUARANTINED (`corpus add-cases`, provenance `synthetic`); they join the
eval pool only after `evol cases promote`.

## Environment

| Var | Meaning |
|-----|---------|
| `EVOL_CASEGEN_PROVIDER` | provider URI (`anthropic://claude-sonnet-5`, `ollama://llama3.2:3b?base_url=…`); falls back to `EVOL_GENERATOR_PROVIDER`, then the anthropic default |
| `EVOL_CASEGEN_TIMEOUT` | per-call deadline (Go duration); falls back to `EVOL_GENERATOR_TIMEOUT`, then 60s |
| provider key env | per scheme via the LLM layer (`ANTHROPIC_API_KEY`, …, or universal `LLM_API_KEY`); key-less providers (ollama) need none |

## Behavior

- One completion call produces up to `count` cases as a strict JSON
  array; a fence-wrapped array is tolerated, prose without an array is a
  dry result (exit 0, zero cases) — not an adapter error.
- Input-less cases are dropped with a diagnostic.
- The client code intentionally mirrors `generator-llm` (adapters are
  self-contained by design); env names differ (`EVOL_CASEGEN_*`).

## Example

```sh
echo '{"evol":"1","port":"generator","action":"synth",
  "artifact":{"ref":"skills/commit-style","body":"..."},
  "knowledge":[{"text":"Dependency bumps use the build type.","source":"notes"}],
  "examples":[],"count":3}' | casegen-llm
```
