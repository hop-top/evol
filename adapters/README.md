# Adapter directories

Repo-internal conventions for the reference adapters that live here.
Nothing in this file is contract: the [port contracts](../spec/README.md)
bind wire behavior only, and third-party adapters name their
executables whatever they like.

## Directory naming

`<role>-<backend>` — role first, backend second.

The role is usually a port shorthand:

| Shorthand | Port |
|-----------|------|
| `artifact` | [ArtifactStore](../spec/port-artifactstore.md) |
| `generator` | [Generator](../spec/port-generator.md) |
| `executor` | [Executor](../spec/port-executor.md) |
| `corpus` | [Corpus](../spec/port-corpus.md) |
| `kb` | [KnowledgeBase](../spec/port-knowledgebase.md) |
| `scorer` | [Scorer](../spec/port-scorer.md) |
| `audit` | [Audit](../spec/port-audit.md) |
| `gate` | Gate (draft, unpublished) |

Three roles name a seam rather than a port: `casegen` (the Generator's
`synth` action), `cases` (Corpus case sourcing), and `runner` (the
reference runner contract below the Executor — not a port at all).

The backend is whatever the adapter shells out to or wraps: a
filesystem (`fs`), a CLI (`tlc`, `ctxt`, `ben`, `eva`), an LLM
provider (`llm`), a cassette runner (`xrr`), and so on.

## Directory contents

Each directory is a standalone `package main` — one executable, no
shared adapter code — with a README mapping port actions to backend
invocations, and tests that exercise the binary over the wire
protocol (one JSON request on stdin, one response on stdout).
