# cases-crtx

Mine eval cases from recorded agent conversations. One normalized input
format — [crtx](https://github.com/hop-top/spec-crtx) v0.1 envelopes —
instead of one ad-hoc parser per CLI's session store. Any runtime whose
sessions are exported to crtx (claude-code, codex, gemini-cli, opencode,
stem, …) becomes a mining source through the same converter.

## Usage

```sh
# files (JSONL of envelopes, or one pretty-printed envelope per file)
cases-crtx sessions/*.jsonl > mined-cases.jsonl

# stdin, topic-filtered, capped
cat sessions.jsonl | cases-crtx --grep 'commit (message|subject)' --limit 50

# inputs only (no assistant answers as expected_output)
cases-crtx --no-expected sessions.jsonl
```

Output: one case per line —
`{id, input, expected_output?, split: "train", provenance: "mined", source: "crtx:<envelope-id>"}`.
Ids are content-addressed (`crtx-<sha256(input)[:12]>`), so identical
inputs dedup across envelopes and re-runs are deterministic.

## Behavior

| Aspect | Rule |
|---|---|
| Pairing | each `user` turn with text pairs with the next `assistant` turn carrying text; intervening `tool` turns are skipped; a new `user` turn closes the window |
| Content | `text` parts only, joined; `thinking` / tool payloads never leak into cases |
| Filtering | `--grep` (case-insensitive regex) on the user turn |
| Secret scrubbing | AWS keys, GitHub tokens/PATs, JWTs, `sk-` keys, Slack tokens, Bearer tokens, PEM blocks → `<redacted:<class>>` in both input and expected; per-class counts on stderr |
| Validation | envelopes with unsupported `crtx_version` or unknown roles are rejected (spec §4/§8); malformed JSONL lines are skipped; both counted on stderr |
| Exit codes | 0 ok · 1 IO/encode failure · 2 usage |

## Deliberately not in v0

- **LLM relevance filtering** — `--grep` only; semantic filtering is later work.
- **Quarantine flow** — mined cases carry `provenance: "mined"` and should
  be *reviewed before entering a gating pool* (see the corpus port's
  provenance mechanism); this tool never writes into a corpus directly.
- **stem SDK** — the envelope shapes are parsed spec-direct with the
  standard library because the SDK module is not publicly importable yet;
  TODO: swap to the stem Go SDK once published, deleting the local structs.
