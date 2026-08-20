# Publishing the port contracts

How the contracts in this directory go from **INTERNAL DRAFT** to
**published**. Publication is a one-commit action once every
precondition below is checked — this file exists so nothing is
rediscovered at flip time.

## Preconditions (all required)

1. **A demonstrated end-to-end improvement.** One artifact, evolved by
   the reference loop, with a holdout gain over baseline that cleared
   the gate — evidenced by the run output, the corpus generation
   records, and the promoted artifact diff. Link the evidence (run log,
   corpus dir snapshot or committed fixtures, artifact diff) in the
   publication commit message.
2. **Draft-row review.** Every field or action annotated *"added while
   INTERNAL DRAFT"* is re-read against the reference implementation and
   the adapters that speak it. Each row is either folded into its base
   table or removed. Grep for the annotation; the sweep is done when it
   returns nothing.
3. **Consistency sweep.** README, port files, and adapter READMEs agree
   on field names, exit codes, and env variables. Known drift is fixed,
   not documented around.
4. **Provenance re-scan.** No internal identifiers, private paths, task
   references, or attribution anywhere under `spec/`.
5. **Tier-2 decision.** Either the Scorer draft has graduated into
   `spec/` (extracted from the working engine seam) or the README
   states explicitly that Tier-2 remains unspecified and why. No silent
   limbo.

## Mechanical steps

1. Fold all *"added while INTERNAL DRAFT"* annotations (precondition 2).
2. Replace the status box in [README.md](README.md): INTERNAL DRAFT →
   published, with the publication date and a pointer to the
   improvement evidence.
3. State the versioning commitment: from this commit, the
   additive/breaking rules in README's "Versioning & stability" section
   are binding; breaking changes bump `evol` and get a migration note.
4. Ship conformance fixtures for any port that has met the
   second-adapter rule (see [conformance-plan.md](conformance-plan.md));
   note per-port fixture status in the README ports table.
5. Announce surface: repository README links the spec as public;
   release notes carry the contract version.

## What publication promises

- Contract-version discipline: `evol: "1"` is stable; anything breaking
  bumps it, with both versions documented during a deprecation window.
- Additive evolution stays cheap: optional fields and new actions may
  land in minor releases; adapters that ignore unknown fields keep
  working, adapters that predate optional actions may error and callers
  degrade.
- The spec describes what adapters can rely on, not what the reference
  engine happens to do this week — engine behavior beyond the contract
  (staging layout, selection policies, gate math) may change freely.

## What publication does NOT promise

- Fixture coverage for every port (second-adapter rule gates each port
  independently).
- Tier-2 contracts (they graduate when extracted, on their own clock).
- Stability of the reference adapters' env variables — those are
  adapter documentation, not contract.
