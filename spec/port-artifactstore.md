# Port: ArtifactStore

> Part of the [evol port contracts](README.md) — INTERNAL DRAFT, `evol: "1"`.

Loads, writes, and versions the artifact under evolution. An artifact is
a text document with structured frontmatter and a prose/instruction
body — a skill file, a prompt, a command definition, or a tool config.

Reference implementations: filesystem skills directory, capability
registry. Any adapter that can resolve a `ref` to versioned text
qualifies — a git repo, a database, a CMS.

## Artifact kinds

| Kind | Example |
|------|---------|
| `skill` | `SKILL.md` in a skills directory |
| `prompt` | system-prompt section file |
| `command` | slash-command definition |
| `tool-config` | tool description / config block |

## Actions

### `load`

Resolve a ref to the current artifact.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `ref` | string | adapter-scoped identifier, e.g. `skills/commit-style` |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `artifact.ref` | string | echoed ref |
| `artifact.kind` | string | one of the kinds above |
| `artifact.frontmatter` | string | raw frontmatter block, empty if none |
| `artifact.body` | string | body text |
| `artifact.version` | string | adapter-defined version id (e.g. content hash, git SHA) |

```json
{"evol": "1", "port": "artifactstore", "action": "load", "ref": "skills/commit-style"}
```

```json
{"evol": "1", "port": "artifactstore", "action": "load",
 "artifact": {"ref": "skills/commit-style", "kind": "skill",
              "frontmatter": "name: commit-style\ndescription: ...",
              "body": "## When to use\n...", "version": "b1946ac9"}}
```

### `write`

Persist a new version of the artifact. The adapter owns versioning
semantics (a git commit, a new row, a timestamped copy).

Request:

| Field | Type | Notes |
|-------|------|-------|
| `ref` | string | target ref |
| `frontmatter` | string | full frontmatter block |
| `body` | string | full body text |
| `message` | string | human-readable change description |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `version` | string | version id of the written artifact |

```json
{"evol": "1", "port": "artifactstore", "action": "write",
 "ref": "skills/commit-style", "frontmatter": "name: commit-style\n...",
 "body": "## When to use\n...", "message": "tighten trigger conditions"}
```

```json
{"evol": "1", "port": "artifactstore", "action": "write", "version": "4f2a11c0"}
```

### `list`

Enumerate available artifact refs, optionally filtered by kind.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `kind` | string? | filter; omit for all kinds |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `refs` | string[] | resolvable refs |

```json
{"evol": "1", "port": "artifactstore", "action": "list", "kind": "skill"}
```

```json
{"evol": "1", "port": "artifactstore", "action": "list",
 "refs": ["skills/commit-style", "skills/review-checklist"]}
```

## Notes

- `load` after `write` must return the newly written version.
- Adapters should treat `ref` as opaque beyond their own namespace
  rules; the engine never parses refs.

See also: [Generator](port-generator.md) consumes loaded artifacts;
[Corpus](port-corpus.md) keys cases and tabu entries by `artifact_ref`.
