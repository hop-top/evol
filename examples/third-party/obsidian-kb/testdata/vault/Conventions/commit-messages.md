---
tags: [conventions, git]
aliases: [commit style, SKILL.md reference]
---
# Commit messages

House rules for commit messages, beyond plain [[Conventions/scopes|Conventional Commits scopes]]:

- Wrap code identifiers in subjects in backticks: `splitFrontmatter`, `config.yaml`.
- Scopes are kebab-case and required when a component is named.
- Bodies are telegraphese: no leading articles, no first person, max three lines.
- Breaking changes carry `!` before the colon AND a "BREAKING CHANGE:" trailer.
- README-only changes are docs, dependency bumps are build, workflow changes are ci.
- Subjects: imperative mood, no trailing period, within 72 characters.
