---
name: commit-messages
description: Writing commit messages for code changes
---

# Commit messages

This skill helps with writing commit messages when committing code.

## When to use

Use this when you need to commit something.

## Guidelines

Commit messages should follow the Conventional Commits idea. That means
there is a type, then a colon, then a description of the change. Types
that exist include feat, fix, refactor, build, ci, chore, docs, style,
perf and test. Sometimes a scope in parentheses goes after the type.

The description should describe the change. Try to keep the subject on
the shorter side, ideally it fits in about seventy two characters or
less. Write it in a way that reads like an instruction rather than a
report of what happened, so prefer wording like "add retry" over
"added retry".

A body can be added after the subject when more explanation helps. The
body can be brief and note-like rather than full prose sentences. Avoid
putting a period at the very end of the subject line.

Do not mention the tools that were used to make the change or who or
what assisted with it. The message is about the change itself.

## Notes

Scopes are lowercase things like module or area names. There is a house
style covering how identifiers appear in subjects, how bodies are
worded, and how breaking changes are marked — follow it. If unsure
about the type, chore is usually safe. Try not to exceed limits.
