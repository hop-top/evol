# Port: KnowledgeBase

> Part of the [evol port contracts](README.md) — published, `evol: "1"`.

Unstructured contextual knowledge: decisions, procedures, findings,
notes. The engine reads it to ground candidate proposals in real usage
(passages passed to the [Generator](port-generator.md) as `knowledge`)
and may write evolution rationale back for humans to find later.

**This port is optional.** Any action may answer
`{"unavailable": true}` instead of its normal response — a knowledge
daemon may be down or simply not configured. The engine must degrade
gracefully: proposals proceed without `knowledge`, appends are dropped,
and tabu falls back to the [Corpus](port-corpus.md) alone. Reference
implementations back onto a knowledge-management daemon; a notes
directory with grep, a wiki, or a vector store all qualify.

## Actions

### `search`

Request:

| Field | Type | Notes |
|-------|------|-------|
| `query` | string | free-text query |
| `limit` | int? | max passages; adapter default if omitted |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `passages` | object[] | `{text, source, score}` |
| `passages[].source` | string | adapter-scoped provenance id |
| `passages[].score` | number | relevance, higher is better; adapter-scaled |

```json
{"evol": "1", "port": "knowledgebase", "action": "search",
 "query": "commit message conventions scoped packages", "limit": 3}
```

```json
{"evol": "1", "port": "knowledgebase", "action": "search",
 "passages": [{"text": "Scoped packages take the scope in parentheses...",
               "source": "notes/conventional-commits", "score": 0.92}]}
```

### `brief`

Composed summary of what the knowledge base holds on a topic.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `topic` | string | |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `text` | string | composed brief |

```json
{"evol": "1", "port": "knowledgebase", "action": "brief",
 "topic": "commit-style skill"}
```

```json
{"evol": "1", "port": "knowledgebase", "action": "brief",
 "text": "Three decisions govern commit style: type prefixes are mandatory..."}
```

### `append`

Write a note back — typically evolution rationale worth remembering.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `text` | string | note body |
| `tags` | string[] | classification tags |

Response: empty object (envelope only).

```json
{"evol": "1", "port": "knowledgebase", "action": "append",
 "text": "add-example strategy beat reorder twice on commit-style; keep examples last",
 "tags": ["evolution", "commit-style"]}
```

```json
{"evol": "1", "port": "knowledgebase", "action": "append"}
```

## Unavailability

```json
{"evol": "1", "port": "knowledgebase", "action": "search", "unavailable": true}
```

Adapters report unavailability per action call; exit code stays 0. Exit
non-zero only for adapter faults (bad config, malformed request) — see
the [wire protocol](README.md#wire-protocol).

## Wire notes (from second-implementer feedback)

- Responses SHOULD echo `evol`, `port`, and `action`; engines MUST NOT
  require the echo (treat it as informational).
- An unset or missing knowledge source is `{"unavailable": true}` with
  exit 0 — adapter errors (non-zero exit) are reserved for malformed
  requests and internal failures.
- A single trailing newline after the response object is permitted.
