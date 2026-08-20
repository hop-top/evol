# Conformance fixtures — layout plan

Design only. **No fixtures ship until a second real adapter exists for
a given port** — that rule is verbatim from the
[spec status box](README.md) and gates each port independently. One
adapter proves the contract is implementable; only a second,
independently built one proves the contract (rather than the first
implementation's quirks) is what's being specified.

## Layout

```
spec/fixtures/
  <port>/
    <action>-<scenario>/
      request.json        # exact bytes written to the adapter's stdin
      response.json       # expected stdout object (see matching rules)
      meta.yaml           # expected exit code + matching options
```

- `meta.yaml` minimum: `exit: 0` (or the expected non-zero for
  adapter-error scenarios) and optional `match:` rules.
- Matching default is structural equality on the response object with
  two relaxations: fields listed under `match.ignore` (adapter-scoped
  values: versions, scores, durations, provenance ids) are compared for
  presence and JSON type only; fields under `match.absent` must not
  appear. Everything else must match exactly.
- Error-plane scenarios (`exit != 0`) have no `response.json`; conformance
  is the exit code plus a non-empty stderr.
- Scenario names are flat and descriptive: `load-happy`,
  `load-missing-ref`, `record-empty-generation`, `search-unavailable`.

## Self-verification without an SDK

A fixture run is three shell steps per scenario, in any language's CI:

```
adapter=<your-binary>
for s in spec/fixtures/<port>/*/ ; do
  out=$("$adapter" < "$s/request.json"); code=$?
  # compare $code to meta.yaml exit, $out to response.json per match rules
done
```

The repo will ship one reference runner script implementing the
comparison rules (POSIX shell + a single-file JSON comparator), so an
adapter author verifies with `spec/fixtures/run.sh <port> <binary>` and
no toolchain beyond their own. The script is part of the fixture
delivery, not a prerequisite for writing adapters.

## Which ports are closest to their second adapter

| Port | First adapter (today) | Likely second | Distance |
|------|----------------------|---------------|----------|
| Corpus | `corpus-fs` (JSONL dir, stdlib-only) | evaluation-store SQLite backend — `corpus-fs`'s own README names it as its replacement behind the same port | closest: the schema mapping already exists in the store |
| Executor | `executor-apx` (layered subprocess / cassette / profile) | container + HTTP-record executor (docker + VCR-style), the "weaker but valid" plain implementation the port text already anticipates | close: contract explicitly designed for both |
| KnowledgeBase | `ctxt-kb` (knowledge daemon CLI) | notes-directory + grep, or an Obsidian vault — the port file itself lists a notes dir, wiki, or vector store as qualifying | close: three actions, all simple |
| ArtifactStore | `fs-artifact` (filesystem, content-hash versions) | git-backed store (SHA versions) — the fs adapter's README defers git-native versioning as a later iteration | medium: same layout, version semantics differ |
| Generator | `generator-llm` (LLM mutation strategies) | a prompt-optimizer bridge (e.g. a DSPy-based proposer) — the port text promises "the engine cannot tell the difference" | furthest: second adapter is a real project, not a variant |

When any port's second adapter lands, that port gets fixtures in the
layout above, and the README ports table gains a fixtures column for it.

## Non-goals

- Multi-language SDKs. The wire protocol is the interface; fixtures are
  the test. Nothing else ships.
- Fixture coverage of engine behavior (selection policies, gate math,
  staging) — fixtures bind adapters to ports, nothing more.
- Golden outputs for LLM-backed adapters' *content* (a generator's
  candidate text is not conformance material; its envelope shape is).
