# Grounded synthetic case generation

`evol cases synth` manufactures new eval cases when you have knowledge
(style guides, decisions, SOPs) but not cases in eval shape. It is the
complement of a corpus adapter over an existing dataset: adapters SERVE
case-shaped data; synthesis PRODUCES cases from non-case-shaped
knowledge.

## Safety model

Two guards keep synthesis honest:

1. **Grounding required.** The engine refuses to synthesize when the
   knowledgebase returns no passages for the artifact (exit 2). Cases
   invented from the artifact text alone are circular evals.
2. **Quarantine always.** Synthesized cases land with
   `provenance: synthetic, quarantined: true` and are invisible to the
   eval pool (`corpus cases` skips them) until a human reviews and runs
   `evol cases promote --artifact <ref> --ids <id,...>`. The loop can
   never generate cases its own mutations trivially pass.

## Flow

```sh
# 1. configure ports.casegen in evol.yaml (see evol.example.yaml)
evol cases synth --config evol.yaml --artifact skills/commit-style --count 5
# 2. review the printed quarantined cases (ids are content-derived syn-…)
evol cases promote --config evol.yaml --artifact skills/commit-style \
  --ids syn-9f2ab61c04d1,syn-1c22ab9e77aa
```

Ports touched: KnowledgeBase (`search`), Generator (`synth`, optional
action — see spec/port-generator.md), Corpus (`add-cases`,
`promote-cases`).
