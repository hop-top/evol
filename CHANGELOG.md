# Changelog

All notable changes documented here.
Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)

## [0.1.0-alpha.2](https://github.com/hop-top/evol/compare/evol/v0.1.0-alpha.1...evol/v0.1.0-alpha.2) (2026-08-20)


### Features

* **adapters:** ben-gate — regression gate over ben runs ([2853cdd](https://github.com/hop-top/evol/commit/2853cdd84ae066befc2fdd530237691e8dbea207))
* **adapters:** cases-crtx — mine eval cases from crtx envelopes ([1e6f006](https://github.com/hop-top/evol/commit/1e6f00686d3d457018e41cd8e825852db1607589))
* **adapters:** ctxt-kb knowledgebase adapter ([6ca8dc7](https://github.com/hop-top/evol/commit/6ca8dc76de9ea4055846ca5b7cf41f75e4eef362))
* **adapters:** eva-backed scorer (draft port) ([a849c85](https://github.com/hop-top/evol/commit/a849c851302cc47019a77695db36cca7a97a9fdf))
* **adapters:** executor-apx — layered subprocess/xrr/aps executor ([961bf62](https://github.com/hop-top/evol/commit/961bf62da14b6ff8efce9eeee4223e05c487a493))
* **adapters:** file-backed corpus port ([2edc602](https://github.com/hop-top/evol/commit/2edc60205799932be0cc4a2ed0406a743ce201f9))
* **adapters:** fs-artifact ArtifactStore adapter ([8ff53c3](https://github.com/hop-top/evol/commit/8ff53c36aded54d7e76f3c8cb745feee6ce2868d))
* **adapters:** fs-artifact git-native versioning + history actions ([848722d](https://github.com/hop-top/evol/commit/848722d0a2e751b34ac7d08aff715bc4d72b9c61))
* **adapters:** generator per-call timeout via EVOL_GENERATOR_TIMEOUT ([5bec639](https://github.com/hop-top/evol/commit/5bec6392fcbe7509fecffe9c89041aff5282c664))
* **adapters:** generator-llm — LLM mutation strategies over Messages API ([0e1a37c](https://github.com/hop-top/evol/commit/0e1a37c7b186554e3d36e0a88033ad3ecea8937c))
* **adapters:** generator-llm on kit ai/llm — provider-agnostic URIs ([dbbabe6](https://github.com/hop-top/evol/commit/dbbabe6d5b58eb32aca57f09cf8e674981e54043))
* **adapters:** runner-xrr — cassette record/replay for runner invocations ([09cd729](https://github.com/hop-top/evol/commit/09cd729e24bb58ae9d6132b6dc91889fb6057f85))
* **cli:** rollback verb + post-promotion hook ([ff33e20](https://github.com/hop-top/evol/commit/ff33e2000299551a8eb23d59dbdb10c7b4b662c4))
* **cmd:** cases review surface + human correction write path ([76096a5](https://github.com/hop-top/evol/commit/76096a5271a9449da41c5ad8923102032fbed836))
* **config:** per-port env in evol.yaml ([c1991ac](https://github.com/hop-top/evol/commit/c1991acef511867add3e04af1a516198724dde91))
* **e2e:** first loop-authored improvement promoted ([c67037c](https://github.com/hop-top/evol/commit/c67037c04d30d86e711078b05d78dff023b2c88d))
* **engine:** audit port + run ledger — evol runs list/show ([51d7915](https://github.com/hop-top/evol/commit/51d79154f3e5099d971c3f3b5b161789dcca9f71))
* **engine:** corrections into gating pool + fixtures on promoted runs ([e6d7e33](https://github.com/hop-top/evol/commit/e6d7e3303b80330e253ec2bfa338823aaa61e808))
* **engine:** drift + kb-churn selection policies ([8fe34db](https://github.com/hop-top/evol/commit/8fe34dbe1a236c0949150914fdfb6078619b651c))
* **engine:** generations loop + port envelope client ([609b02e](https://github.com/hop-top/evol/commit/609b02ebcd868581068949a1c09dcbe9384e8a6e))
* **engine:** kb-churn on real timestamps ([1a71b00](https://github.com/hop-top/evol/commit/1a71b0019dc7b642a78bc821ed1f38fb840aa044))
* **engine:** provider sweep + paired-bootstrap significance gate ([4aa3867](https://github.com/hop-top/evol/commit/4aa38678ea3db19004250e521e2e55f1da949d99))
* **engine:** target selection — evol targets + run --select ([a13c1fa](https://github.com/hop-top/evol/commit/a13c1fab2c624145238b416307b60ec393221fd2))
* **executor:** provider passthrough + runner contract with per-tool shims ([5ab724d](https://github.com/hop-top/evol/commit/5ab724ddc7434155494338e5aef1ee25fe4368a9))
* grounded synthetic case generation with quarantine ([6dc33f9](https://github.com/hop-top/evol/commit/6dc33f971e77b77e436926d28a48aeb575a612db))
* **routing:** evidence -&gt; pool config write-back ([5c5cc75](https://github.com/hop-top/evol/commit/5c5cc7580e012d57922f82fd5482496f2eb83dcb))


### Bug Fixes

* **adapters:** cases-crtx lint debt masked by stale lint cache ([a3f67cc](https://github.com/hop-top/evol/commit/a3f67cc850dec17bce57169948867341142485f9))
* **adapters:** generator output format robust for small models ([9bcfaef](https://github.com/hop-top/evol/commit/9bcfaef2ea453d12329835c00101eeaeef616e09))
* **cli:** runs verbs need only the audit port ([6abe1f6](https://github.com/hop-top/evol/commit/6abe1f625904eb2b8241caa3d93318b7103cca4c))
* **config:** kit layered loader preserves env key case ([79d5ddb](https://github.com/hop-top/evol/commit/79d5ddbf0f9c4a3d6c96ba0fb2d349f7bb2e7543))
* **engine:** persist candidate strategy in corpus records ([4bb5365](https://github.com/hop-top/evol/commit/4bb5365f0950bae22416951391499cbd916786a1))

## [0.1.0-alpha.1](https://github.com/hop-top/evol/compare/evol/v0.1.0-alpha.0...evol/v0.1.0-alpha.1) (2026-08-20)


### Features

* **adapters:** ben-gate — regression gate over ben runs ([2853cdd](https://github.com/hop-top/evol/commit/2853cdd84ae066befc2fdd530237691e8dbea207))
* **adapters:** cases-crtx — mine eval cases from crtx envelopes ([1e6f006](https://github.com/hop-top/evol/commit/1e6f00686d3d457018e41cd8e825852db1607589))
* **adapters:** ctxt-kb knowledgebase adapter ([6ca8dc7](https://github.com/hop-top/evol/commit/6ca8dc76de9ea4055846ca5b7cf41f75e4eef362))
* **adapters:** eva-backed scorer (draft port) ([a849c85](https://github.com/hop-top/evol/commit/a849c851302cc47019a77695db36cca7a97a9fdf))
* **adapters:** executor-apx — layered subprocess/xrr/aps executor ([961bf62](https://github.com/hop-top/evol/commit/961bf62da14b6ff8efce9eeee4223e05c487a493))
* **adapters:** file-backed corpus port ([2edc602](https://github.com/hop-top/evol/commit/2edc60205799932be0cc4a2ed0406a743ce201f9))
* **adapters:** fs-artifact ArtifactStore adapter ([8ff53c3](https://github.com/hop-top/evol/commit/8ff53c36aded54d7e76f3c8cb745feee6ce2868d))
* **adapters:** generator per-call timeout via EVOL_GENERATOR_TIMEOUT ([5bec639](https://github.com/hop-top/evol/commit/5bec6392fcbe7509fecffe9c89041aff5282c664))
* **adapters:** generator-llm — LLM mutation strategies over Messages API ([0e1a37c](https://github.com/hop-top/evol/commit/0e1a37c7b186554e3d36e0a88033ad3ecea8937c))
* **adapters:** generator-llm on kit ai/llm — provider-agnostic URIs ([dbbabe6](https://github.com/hop-top/evol/commit/dbbabe6d5b58eb32aca57f09cf8e674981e54043))
* **adapters:** runner-xrr — cassette record/replay for runner invocations ([09cd729](https://github.com/hop-top/evol/commit/09cd729e24bb58ae9d6132b6dc91889fb6057f85))
* **e2e:** first loop-authored improvement promoted ([c67037c](https://github.com/hop-top/evol/commit/c67037c04d30d86e711078b05d78dff023b2c88d))
* **engine:** corrections into gating pool + fixtures on promoted runs ([e6d7e33](https://github.com/hop-top/evol/commit/e6d7e3303b80330e253ec2bfa338823aaa61e808))
* **engine:** generations loop + port envelope client ([609b02e](https://github.com/hop-top/evol/commit/609b02ebcd868581068949a1c09dcbe9384e8a6e))
* **engine:** provider sweep + paired-bootstrap significance gate ([4aa3867](https://github.com/hop-top/evol/commit/4aa38678ea3db19004250e521e2e55f1da949d99))
* **engine:** target selection — evol targets + run --select ([a13c1fa](https://github.com/hop-top/evol/commit/a13c1fab2c624145238b416307b60ec393221fd2))
* **executor:** provider passthrough + runner contract with per-tool shims ([5ab724d](https://github.com/hop-top/evol/commit/5ab724ddc7434155494338e5aef1ee25fe4368a9))


### Bug Fixes

* **adapters:** cases-crtx lint debt masked by stale lint cache ([a3f67cc](https://github.com/hop-top/evol/commit/a3f67cc850dec17bce57169948867341142485f9))
* **adapters:** generator output format robust for small models ([9bcfaef](https://github.com/hop-top/evol/commit/9bcfaef2ea453d12329835c00101eeaeef616e09))

## [Unreleased]
