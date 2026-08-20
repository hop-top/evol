---
name: commit-messages
description: Writing commit messages for code changes
---
# Commit Messages

Use this skill when writing commit messages. Follow Conventional Commits format: `<type>(<scope>): <subject>`. Include an optional body for explanation.

## Format & Types
Choose a type from: feat, fix, refactor, build, ci, chore, docs, style, perf, test. Use `chore` if unsure. Scopes (kebab-case) are required when referencing a component or module, and otherwise go in parentheses after the type. Wrap code identifiers (`func`, `module`, `config`) in backticks.

## Subject Line Rules
- Keep it concise (ideally ≤72 chars).
- Use imperative mood ("add feature", not "added feature").
- Omit trailing periods.
- Append `!` before the colon for breaking changes (e.g., `feat(api)!: ...`).

## Body Rules
- Brief, note-like fragments; full prose sentences are discouraged.
- Max 3 lines. No leading articles (`the`, `a`, `an`) or first-person pronouns.
- For breaking changes, add a "BREAKING CHANGE" footer describing the impact.

## Constraints
Never mention tools, authors, or assistive AI in the message. Focus solely on the code change.