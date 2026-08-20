# Promotion, versioning, and rollback

What happens after a candidate clears the gate — and how to undo it.

## The promotion path

1. The engine writes the accepted candidate through the ArtifactStore
   port; the store returns a `version` (and, git-native, a
   `git_commit`).
2. The corpus records the accepted outcome (with fixtures when
   configured) — the audit trail of *why*.
3. The CLI runs the operator's post-promotion hook, if configured —
   the plumbing of *what next*.

## Git-native versioning

The reference filesystem adapter commits every promotion when both
hold:

- `EVOL_ARTIFACT_GIT=1` in the adapter's environment
- the artifact root resolves inside a git work tree

Then:

- `write` → `git add <ref> && git commit -m "evol: promote <ref> <version>" -- <ref>`,
  refusing cleanly if unrelated staged changes are present
- the write response carries `git_commit` alongside the content-hash
  `version`; `evol run` surfaces it as `git_commit` in the result JSON
- author identity comes from ordinary git config — the adapter invents
  nothing

Without git, promotions still version by content hash; only history
listing and restore need the git mode.

## Rollback

```sh
evol rollback --config evol.yaml --artifact skills/commit-style/SKILL.md
# or a specific target:
evol rollback --config evol.yaml --artifact ... --to <version-or-sha-prefix>
```

Default target is the version immediately before the latest — undoing
the most recent promotion. A rollback is a **forward commit**
(`evol: rollback <ref> to <version>`), never a history rewrite: the
promotion, its revert, and the reasons for both stay in the log.

## Post-promotion hook

```yaml
promotion:
  hook: ["./scripts/on-promote.sh"]
```

The hook runs after a successful promotion with:

| Env | Value |
|-----|-------|
| `EVOL_PROMOTED_REF` | the promoted artifact ref |
| `EVOL_PROMOTED_VERSION` | its new version id |
| `EVOL_PROMOTED_GIT_COMMIT` | promotion commit (may be empty) |

A non-zero hook exit logs a warning and never fails the promotion —
the improvement is already real and recorded.

No tool is privileged: the hook is plain argv. Typical consumers are a
package publish step, a capability install into an agent profile, or a
notification — whatever the operator wires. Repository release
processes (tags, release PRs) are deliberately **not** part of this
path: releases are a separate, explicitly operator-gated act.
