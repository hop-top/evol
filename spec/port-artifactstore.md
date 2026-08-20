# Port: ArtifactStore

> Part of the [evol port contracts](README.md) — published, `evol: "1"`.

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
| `git_commit` | string? | commit SHA when the adapter runs git-native versioning; omitted otherwise |

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

### `versions` (optional)

List an artifact's version history, newest first. Adapters without
version history MAY return an adapter error (non-zero exit) with a
diagnostic naming what would enable it; callers degrade with that
guidance. Additive-optional action per the
[versioning rules](README.md).

Request:

| Field | Type | Notes |
|-------|------|-------|
| `ref` | string | target ref |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `versions` | object[] | newest first |
| `versions[].version` | string | version id (e.g. content hash) |
| `versions[].git_commit` | string? | producing commit, git-native adapters |

### `restore` (optional)

Restore the artifact to a prior version. Restoration is itself a new
version — never a history rewrite. Same optionality rules as
`versions`.

Request:

| Field | Type | Notes |
|-------|------|-------|
| `ref` | string | target ref |
| `version` | string | version id or (git-native) commit-SHA prefix |

Response:

| Field | Type | Notes |
|-------|------|-------|
| `version` | string | version id of the restored content |
| `git_commit` | string? | rollback commit, git-native adapters |

## Notes

- `load` after `write` must return the newly written version.
- Adapters should treat `ref` as opaque beyond their own namespace
  rules; the engine never parses refs.
- **Git-native mode (reference filesystem adapter):** with
  `EVOL_ARTIFACT_GIT=1` and the artifact root inside a git work tree,
  every `write` also stages and commits the ref (refusing when
  unrelated staged changes are present), `git_commit` is returned
  alongside the content-hash version, and `versions`/`restore` serve
  the ref's git history. Rollbacks are forward commits. Without the
  env or a work tree, behavior is unchanged and the history actions
  error cleanly.

See also: [Generator](port-generator.md) consumes loaded artifacts;
[Corpus](port-corpus.md) keys cases and tabu entries by `artifact_ref`.
