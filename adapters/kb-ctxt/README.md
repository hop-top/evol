# kb-ctxt

KnowledgeBase port adapter backed by the ctxt CLI. Implements `spec/port-knowledgebase.md`: one JSON request on
stdin, one JSON response on stdout.

## Verb mapping

| Port action | ctxt invocation |
|---|---|
| availability probe | `ctxt status --format json` (exit 0 = healthy/degraded) |
| `search` | `ctxt find <query> --format json --limit <n> --no-hints --quiet` |
| `brief` | `ctxt compose brief --tagged <topic> --quiet --no-hints`; on failure falls back to a joined view of top `search` passages |
| `append` | `ctxt analyze <text> --hint <tag,tag> --quiet --no-hints` |

Passage mapping from `find` output: `text` = `text_content` (falls back
to `raw_content`), truncated to 700 runes; `source` = object `id`;
`score` = `metadata.rrf_score` (higher is better). Objects with no
content are dropped.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `EVOL_CTXT_BIN` | `ctxt` | binary to shell out to (tests point this at fakes) |
| `EVOL_CTXT_TIMEOUT_MS` | `15000` | per-action call timeout |
| `EVOL_CTXT_PROBE_TIMEOUT_MS` | `5000` | availability probe timeout |

## Degradation semantics

Daemon unreachable, binary missing, call timeout, or unparseable output
→ `{"unavailable": true}` with **exit 0**. The engine proceeds without
knowledge; appends are dropped (a diagnostic lands on stderr). Non-zero
exit is reserved for adapter faults: malformed request, wrong port,
unknown action, missing required fields.

## Example

```sh
echo '{"evol":"1","port":"knowledgebase","action":"search","query":"commit conventions","limit":3}' \
  | kb-ctxt
```

## Deliberately not implemented (v0)

- No pagination or cursor support on `search`.
- No per-profile scoping (`--profile`) — single default profile.
- `brief` topics map to ctxt tags; free-text topics that match no tag
  get the search-join fallback rather than a composed brief.
