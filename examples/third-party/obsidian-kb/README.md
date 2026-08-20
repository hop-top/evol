# Obsidian vault → evol KnowledgeBase adapter

A third-party-style implementation of the evol KnowledgeBase port:
one Python 3 file, standard library only, zero evol code imported.
Written against `spec/port-knowledgebase.md` + `spec/README.md` alone,
as an outside adapter author would.

## Use with the engine

```yaml
# evol.yaml
ports:
  knowledgebase:
    cmd: ["python3", "examples/third-party/obsidian-kb/obsidian_kb.py"]
    timeout_seconds: 30
```

```sh
export OBSIDIAN_VAULT=~/Documents/MyVault
evol run --config evol.yaml --artifact <ref>
```

The engine queries `search` with the artifact ref verbatim; passages
from your vault land in the generator's `knowledge` — grounded
proposals from your own notes.

## What it took to implement the port

Contract read-to-working-adapter: roughly one sitting. The wire
protocol (one JSON in, one JSON out, exit-code planes) held with no
surprises; the fixture tests below passed against the spec examples
unmodified. Genuine ambiguities an outsider hits — feedback for the
spec authors:

1. **Unset configuration: unavailable or adapter error?** The port
   file says a KB "may be down or simply not configured" (→
   `unavailable`), while the wire protocol calls bad config an adapter
   error (→ non-zero). This adapter chose `unavailable` for a
   missing/unset vault. The spec should pick one.
2. **Envelope echo in responses** is shown in every example but never
   stated as a requirement. This adapter echoes; a stricter reader
   might not.
3. **`score` scale** is "adapter-scaled" — fine — but nothing says
   whether the engine treats it ordinally or cardinally. Normalized
   0..1 here, top hit = 1.0.
4. **`append` targeting**: no guidance on where writes should land.
   Chose `Inbox/evol.md`, hashtags from `tags`.
5. **Trailing newline after the response object**: allowed? Assumed
   yes (JSON decoders tolerate it); worth one sentence in the spec.

## Behavior

- `search` — term-frequency relevance with log-dampened length
  normalization (raw tf/len let a 10-token stub outrank the
  authoritative note; the test suite caught it), filename boost,
  frontmatter skipped, `[[wikilinks]]` unwrapped in snippets.
- `brief` — top-3 search sections joined with source attribution.
- `append` — appends to `Inbox/evol.md` with `#tags`.
- Vault missing/unset → `{"unavailable": true}`, exit 0.
- Malformed request / wrong port / unknown action → stderr + exit 1.

## Tests and fixtures

```sh
python3 test_obsidian_kb.py             # 12 wire-level tests, subprocess-real
```

`fixtures/knowledgebase/` holds request/response pairs from real runs
of this adapter in the layout `spec/conformance-plan.md` describes
(`<action>-<scenario>/{request.json,response.json,meta.yaml}`). As the
KB port's second independently built adapter, these seed the port's
official conformance fixtures.

## Limitations

Naive lexical relevance (no embeddings, no link-graph weighting);
snippets are fixed-radius windows; `brief` is composition-free
concatenation. All fine for grounding; none of it is the port's
business.
