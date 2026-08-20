# Gen-1 e2e: evolve the commit-messages skill

Everything runs from the repo root. The artifact under evolution is
`e2e/artifacts/commit-messages/SKILL.md` — deliberately mediocre; the
loop's job is to measurably improve holdout scores.

## 1. Build the engine + port binaries (gitignored)

```sh
mise exec -- go build -o e2e/bin/evol .
for a in fs-artifact generator-llm executor-apx corpus-fs scorer-eva; do
  mise exec -- go build -o e2e/bin/$a ./adapters/$a
done
```

## 2. Seed the corpus

```sh
export EVOL_CORPUS_ROOT=e2e/.corpus
e2e/bin/seed-corpus.sh commit-messages/SKILL.md
```

corpus-fs keys stores by `sha256(artifact_ref)[:12]`; the script computes
the dir and copies `e2e/cases/cases.jsonl` (8 train / 4 holdout) into it.

## 3. Environment

```sh
export EVOL_ARTIFACT_ROOT=e2e/artifacts
export EVOL_CORPUS_ROOT=e2e/.corpus
export EVOL_EXEC_CMD='["e2e/bin/runners/claude.sh"]'   # runner shim; see "Runner selection"
# agent-under-test model: set executor_provider in e2e/evol.yaml
# (claude://haiku default; ollama://… etc. per the runner table)
# Generator (pick one):
export ANTHROPIC_API_KEY=...             # Messages API
# or, once the provider-URI generator lands:
# export EVOL_GENERATOR_PROVIDER='ollama://<model>?base_url=http://localhost:11434'
```

The engine passes its environment through to every adapter.

## 4. Verify wiring (no LLM calls)

```sh
e2e/bin/evol run --config e2e/evol.yaml --dry-run --format json
```

Verified at scaffold time: exits 0, prints the resolved plan (ports,
thresholds delta 0.05 / trials 2, budget 3×3, holdout split).

## 5. Live run

```sh
e2e/bin/evol run --config e2e/evol.yaml
```

Exit codes: 0 promoted · 1 no improvement · 2 gate precondition ·
3 config/adapter error.

## What proves improvement

- Baseline vs promoted holdout mean: printed by the run; both are also
  in `e2e/.corpus/<hash>/generations.jsonl` (every candidate, every
  verdict, accepted and rejected alike).
- The promoted skill is written back to
  `e2e/artifacts/commit-messages/SKILL.md` (diff it against git).
- Re-running step 5 starts from the improved artifact; tabu prevents
  re-proposing rejected strategies.

## Scorer note (observed at scaffold time)

The default scorer is `e2e/bin/score-commit.py` — stdlib fallback
mirroring `e2e/contracts/commit-subject.yaml` (same five checks).

The eva-backed adapter (`e2e/bin/scorer-eva`) requires an eva build with
standalone contract mode (`eva run --contract <yaml> --input - --format
json`). The eva 0.1.0a1 build installed here predates it: `eva run` only
accepts `--dataset/--target` and exits 2 with a usage error on both
streams (observed). `eva contract validate` works and the committed
contract passes it. Once a newer eva is installed: verify
`eva run --help` lists `--contract/--input`, then swap the scorer cmd in
`e2e/evol.yaml` to `[e2e/bin/scorer-eva]` and
`export EVOL_EVA_CONTRACT=e2e/contracts/commit-subject.yaml`.

## Executor note

Gen-1 runs the plain-subprocess layer (`executor_mode: live`) — no
frozen environment. The +xrr layer (cassette record/replay) and +aps
layer (per-candidate profiles) are configured per
`adapters/executor-apx/README.md` when determinism is required;
`fixtures_dir` in `e2e/evol.yaml` then records the cassette path with
every promoted candidate.

## Recording runs (cassettes)

Wrap any runner shim with `runner-xrr` to cassette-record every
(candidate content, case input, provider) triple, so future runs —
regression re-runs, or re-evaluating the same candidates after a
target-selection or scorer change — replay from disk instead of
re-spending agent calls:

```sh
go build -buildvcs=false -o e2e/bin/runner-xrr ./adapters/runner-xrr
export EVOL_EXEC_CMD='["e2e/bin/runner-xrr","e2e/bin/runners/claude.sh"]'
export XRR_MODE=record          # cache-as-you-go during evolution runs
export XRR_CASSETTE_DIR=e2e/cassettes
```

- `record` during evolution runs: live calls happen once, each pair is
  persisted. New candidates always execute live (their content is new).
- `replay` for regression re-runs of recorded artifacts: no agent
  spawns; a miss exits 21 (recorded pairs only).
- Cassette identity is candidate CONTENT + case input + provider —
  temp staging paths never leak into keys (`adapters/runner-xrr/README.md`).
- Committed cassettes under `e2e/cassettes/` double as the regression
  fixtures that `fixtures_dir` records with promoted candidates.

## Runner selection

The agent under test runs behind the **reference runner contract**
(spec/port-executor.md): stdin = case input, `EVOL_CANDIDATE_REF` =
candidate body path, `EVOL_PROVIDER` = optional provider URI, stdout =
agent output only, non-zero exit = run failure. One shim per tool lives
in `e2e/bin/runners/` — no tool is privileged; swap by pointing
`EVOL_EXEC_CMD` (and `ports.executor` prep) at a different shim and
setting `executor_provider` in `e2e/evol.yaml`:

```sh
export EVOL_EXEC_CMD='["e2e/bin/runners/claude.sh"]'   # pick your shim
# evol.yaml: executor_provider: claude://haiku          # pick your model
```

A shim handed a scheme it does not speak fails fast (exit 64) rather
than silently running the wrong model.

Shim status — smoked once each at change time against the baseline
skill (class: agent-cli = full coding agent; llm-pipe = prompt-through-model):

| Shim | Class | System injection | Provider schemes | Smoke status |
|---|---|---|---|---|
| `runners/claude.sh` | agent-cli | native (`--append-system-prompt`) | `claude://<model>` | ✅ `claude://haiku` → `fix(config): prevent nil deref when config file is missing` |
| `runners/codex.sh` | agent-cli | prompt-prefix | `codex://<model>`, `ollama://<model>` (`--oss`) | ✅ default backend → `build: upgrade database driver to v3` (clean via `--output-last-message`); ✗ local `llama3.2:3b` rejected — codex requires a thinking-capable model |
| `runners/gemini.sh` | agent-cli | prompt-prefix | `gemini://<model>` | ⚠️ UNTESTED — headless call failed with an auth/project-eligibility error on this machine |
| `runners/opencode.sh` | agent-cli | prompt-prefix | `opencode://<provider/model>`, `ollama://<model>` | ⚠️ UNTESTED — no ollama provider configured in opencode here |
| `runners/ollama.sh` | llm-pipe | native (chat `system` message) | `ollama://<model>?base_url=<url>` | ✅ `llama3.2:3b` @ `127.0.0.1:11500` → `feat(fetch): add retry with exponential backoff` |
| `runners/fabric.sh` | llm-pipe | prompt-prefix (patterns live in user config; not touched) | `ollama://<m>`, `anthropic://<m>` (URI params ignored) | ✅ `Ollama\|llama3.2:3b` → `feat: add rate limiting to public API endpoints` (unrelated GitHub-provider warning on stderr) |
| `runners/foo.sh` | llm-pipe | native (hermetic pattern + pool config under temp `XDG_CONFIG_HOME`) | any kit/llm URI | ⚠️ UNTESTED — this foo build has no local-model/base_url support (its docs say so); hosted schemes need a provider key. Shim is ready for when support lands |
| `runners/llm.sh` | llm-pipe | native (`-s`) | `llm://<m>`, `ollama://<m>` (plugin) | ⚠️ UNTESTED — local `llm` install is broken (pipx interpreter missing) |

UNTESTED means exactly that: the shim encodes the tool's documented
interface but has not produced a live commit message on this machine —
statuses above are honest, not aspirational.
