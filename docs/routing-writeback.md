# Routing write-back

The second half of the model-dimension design: per-model evaluation
results become routing configuration, and the routing configuration
becomes an artifact the loop can evolve.

## The loop

```
executor_providers: [primary, secondary...]      (evol.yaml)
        │
        ▼
engine sweep — every candidate (and the baseline) is additionally
scored under each secondary provider; results land in the corpus as
provider-attributed rows (verdict "evidence"); gated rows carry the
primary provider
        │
        ▼
evol routing emit --artifact <ref>
  aggregates every provider-attributed row into per-provider means
        │
        ▼
routing adapter ("routing" port, action "emit")
  writes a kit-llm pool fragment: entries sorted by mean, weights
  normalized to the best performer, credentials stripped
        │
        ▼
pool YAML — routing configuration. It is a tool-config artifact:
the artifact store serves that kind, so the loop can evolve the
router's own config with the same gate that evolves skills.
```

## What counts as evidence

`evol routing emit` aggregates **every row that carries a provider
URI** — sweep rows (verdict `evidence`) and gated rows (accepted /
rejected under the primary) alike. The primary provider's gated rows
are its evidence; excluding them would leave the primary out of the
pool entirely. Rows without a provider (single-provider runs before
provider attribution existed) are skipped.

## v0 limitations — honest list

- **Direct store read.** The corpus port exposes no action returning
  evidence rows (`history` deliberately excludes them, `tabu` returns
  rejects only), so the command reads the corpus-fs layout directly
  (`$EVOL_CORPUS_ROOT/<sha256(ref)[:12]>/generations.jsonl`). This
  couples the verb to the file-backed corpus adapter. Proposed port
  addition for the next contract revision:

  ```
  action "evidence"
  request  {artifact_ref}
  response {rows: [{provider, scores: [{case_id, score}], verdict,
            generation}]}
  ```

  Returns every provider-attributed row, evidence and gated alike.
  Once a corpus adapter serves it, `evol routing emit` switches to the
  port and the direct read is deleted.
- **No significance weighting.** Weights are normalized means; n is
  carried but not yet used to discount low-sample providers.
- **No score-drift decay.** Old evidence weighs the same as new; a
  recency discount belongs with the self-scheduling drift work.
- **One artifact per pool.** Cross-artifact aggregation (one pool from
  many skills' evidence) is a deliberate non-goal until a real
  multi-artifact corpus exists.

## Usage

```sh
export EVOL_CORPUS_ROOT=e2e/.corpus
go build -o e2e/bin/routing-emit ./adapters/routing-emit
evol routing emit --config e2e/evol.yaml \
  --artifact commit-messages/SKILL.md \
  --adapter '["e2e/bin/routing-emit"]' \
  --out .evol/routing-pool.yaml
```

The adapter refuses to write outside the working tree unless
`EVOL_ROUTING_ALLOW_ABS=1`.
