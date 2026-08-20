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
export EVOL_EXEC_CMD='["e2e/bin/agent-exec.sh","{candidate_ref}","{input}"]'
export EVOL_AGENT_MODEL=haiku            # agent-under-test model (claude -p)
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
