# fs-artifact

ArtifactStore adapter over a plain filesystem directory. Implements the
[artifactstore port contract](../../spec/port-artifactstore.md): one JSON
request on stdin, one JSON response on stdout, non-zero exit with stderr
diagnostics on any adapter error.

## Configuration

| Env var | Required | Meaning |
|---------|----------|---------|
| `EVOL_ARTIFACT_ROOT` | yes | directory that refs resolve against |

Refs are root-relative slash paths; absolute refs and `..` escapes are
rejected, for reads and writes alike.

## Kind conventions

| Kind | Matches |
|------|---------|
| `skill` | any `SKILL.md`, anywhere under the root |
| `prompt` | `prompts/**/*.md` |
| `command` | `commands/**/*.md` |
| `tool-config` | `tool-configs/**/*.{md,yaml,yml,json,toml}` |

Files matching none of these are invisible to `list`. Hidden directories
(`.git`, …) are skipped.

## Invocation

```sh
echo '{"evol":"1","port":"artifactstore","action":"load","ref":"skills/commit-style/SKILL.md"}' \
  | EVOL_ARTIFACT_ROOT=/path/to/artifacts fs-artifact
```

Writes are atomic (temp file + rename in the target directory). The
`write.message` field is echoed to stderr as a diagnostic only.

## Deliberately not implemented (v0)

- Git-native versioning: `version` is the first 8 hex chars of the
  content sha256, not a commit SHA. A git-backed adapter is a later
  iteration; the contract already allows it (`version` is opaque).
- Concurrent-writer coordination: last rename wins.
- Frontmatter parsing: the block is passed through raw; the adapter
  never interprets YAML.
