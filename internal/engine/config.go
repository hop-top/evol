package engine

import (
	"fmt"
	"strings"
	"time"

	"hop.top/evol/internal/port"
)

// PortConfig binds one port to an adapter command.
type PortConfig struct {
	// Cmd is the adapter argv; Cmd[0] is the executable.
	Cmd []string `mapstructure:"cmd" json:"cmd"`
	// TimeoutSeconds bounds each call; 0 uses the port default.
	TimeoutSeconds int `mapstructure:"timeout_seconds" json:"timeout_seconds,omitempty"`
	// Env is adapter environment provided by config; a variable set in
	// the engine's process environment overrides the same key here.
	Env map[string]string `mapstructure:"env" json:"env,omitempty"`
}

func (p PortConfig) Client(name string) *port.Client {
	return &port.Client{
		Port:    name,
		Cmd:     p.Cmd,
		Timeout: time.Duration(p.TimeoutSeconds) * time.Second,
		Env:     p.Env,
	}
}

// Config is the engine's resolved configuration (evol.yaml).
type Config struct {
	// Artifact is the default artifact ref to evolve; the --artifact
	// flag overrides it.
	Artifact string `mapstructure:"artifact" json:"artifact"`

	Ports struct {
		ArtifactStore PortConfig `mapstructure:"artifactstore" json:"artifactstore"`
		Generator     PortConfig `mapstructure:"generator" json:"generator"`
		Executor      PortConfig `mapstructure:"executor" json:"executor"`
		Corpus        PortConfig `mapstructure:"corpus" json:"corpus"`
		// Scorer scores transcripts against cases (spec/port-scorer.md).
		Scorer PortConfig `mapstructure:"scorer" json:"scorer"`
		// KnowledgeBase is optional; leave cmd empty to disable.
		KnowledgeBase PortConfig `mapstructure:"knowledgebase" json:"knowledgebase"`
		// CaseGen is optional: the generator-port `synth` action for
		// grounded synthetic case generation (`evol cases synth`).
		CaseGen PortConfig `mapstructure:"casegen" json:"casegen,omitempty"`
		// Audit is optional: the run ledger (spec/port-audit.md). With
		// no cmd the engine runs unaudited and notes it once per run.
		Audit PortConfig `mapstructure:"audit" json:"audit,omitempty"`
	} `mapstructure:"ports" json:"ports"`

	Thresholds struct {
		// Delta is the required improvement over the baseline mean.
		Delta float64 `mapstructure:"delta" json:"delta"`
		// Trials is how many times each case is run and scored;
		// scores average across trials. Minimum 1.
		Trials int `mapstructure:"trials" json:"trials"`
		// SigLevel is the paired-bootstrap significance level the
		// acceptance gate requires in addition to the mean delta
		// (p ≤ sig_level). Defaults to 0.05. Significance testing is
		// automatically disabled below sigMinPairs paired cases —
		// mean-only gating with a logged warning.
		SigLevel float64 `mapstructure:"sig_level" json:"sig_level"`
		// SigSeed seeds the bootstrap resampler so p-values are
		// reproducible. Defaults to 1.
		SigSeed int64 `mapstructure:"sig_seed" json:"sig_seed"`
	} `mapstructure:"thresholds" json:"thresholds"`

	Budget struct {
		// Generations bounds the propose→evaluate loop. Minimum 1.
		Generations int `mapstructure:"generations" json:"generations"`
		// MaxCandidates bounds candidates per generation. Minimum 1.
		MaxCandidates int `mapstructure:"max_candidates" json:"max_candidates"`
	} `mapstructure:"budget" json:"budget"`

	// Holdout names the corpus split used for gating. Defaults to
	// "holdout".
	Holdout string `mapstructure:"holdout" json:"holdout"`

	// ExecutorMode is the env.mode hint sent to the Executor:
	// replay, record, or live. Defaults to "replay".
	ExecutorMode string `mapstructure:"executor_mode" json:"executor_mode"`

	// ExecutorProvider, when set, is forwarded as env.provider on every
	// Executor run — a model/provider URI for the agent under test
	// (e.g. claude://haiku, ollama://llama3.2:3b?base_url=…). Optional;
	// interpretation belongs to the executor's run wrapper.
	ExecutorProvider string `mapstructure:"executor_provider" json:"executor_provider,omitempty"`

	// ExecutorProviders, when set, sweeps the case × trial matrix once
	// per provider URI and records per-provider results to the corpus.
	// The FIRST entry is the primary provider: the gate compares the
	// candidate against the baseline under it alone (fair A/B); every
	// other provider's scores are recorded as evidence rows only.
	// Overrides ExecutorProvider.
	ExecutorProviders []string `mapstructure:"executor_providers" json:"executor_providers,omitempty"`

	// FixturesDir, when set, is recorded with the promoted candidate's
	// corpus entry as fixtures.cassette_dir — the operator-configured
	// location of the recorded environment (adapter-agnostic; the
	// engine never inspects it).
	FixturesDir string `mapstructure:"fixtures_dir" json:"fixtures_dir,omitempty"`

	// Promotion configures post-promotion behavior.
	Promotion struct {
		// Hook is an operator-configured argv executed by the CLI after a
		// successful promotion, with EVOL_PROMOTED_REF,
		// EVOL_PROMOTED_VERSION, and EVOL_PROMOTED_GIT_COMMIT (possibly
		// empty) in its environment. A non-zero hook exit logs a warning
		// and never fails the promotion — the improvement is already
		// real. No tool is privileged: publish steps, capability
		// installs, notifications are all just argv.
		Hook []string `mapstructure:"hook" json:"hook,omitempty"`
	} `mapstructure:"promotion" json:"promotion,omitempty"`
}

// primaryProvider is the provider URI the gate compares under: the
// first executor_providers entry when sweeping, else the single
// executor_provider (possibly empty).
func (c *Config) primaryProvider() string {
	if len(c.ExecutorProviders) > 0 {
		return c.ExecutorProviders[0]
	}
	return c.ExecutorProvider
}

// secondaryProviders are the sweep-only providers: everything after the
// primary. Their scores are recorded as evidence, never gated on.
func (c *Config) secondaryProviders() []string {
	if len(c.ExecutorProviders) > 1 {
		return c.ExecutorProviders[1:]
	}
	return nil
}

// Normalize applies defaults and validates the parts the engine cannot
// run without. It returns a descriptive error for config faults (exit
// class 3 at the CLI).
func (c *Config) Normalize() error {
	if c.Thresholds.Trials < 1 {
		c.Thresholds.Trials = 1
	}
	if c.Budget.Generations < 1 {
		c.Budget.Generations = 1
	}
	if c.Budget.MaxCandidates < 1 {
		c.Budget.MaxCandidates = 1
	}
	if c.Holdout == "" {
		c.Holdout = "holdout"
	}
	if c.ExecutorMode == "" {
		c.ExecutorMode = "replay"
	}
	if c.Thresholds.SigLevel == 0 {
		c.Thresholds.SigLevel = 0.05
	}
	if c.Thresholds.SigLevel < 0 || c.Thresholds.SigLevel > 1 {
		return fmt.Errorf("config: thresholds.sig_level %v outside (0, 1]", c.Thresholds.SigLevel)
	}
	if c.Thresholds.SigSeed == 0 {
		c.Thresholds.SigSeed = 1
	}
	seen := make(map[string]bool, len(c.ExecutorProviders))
	for _, p := range c.ExecutorProviders {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("config: executor_providers contains an empty entry")
		}
		if seen[p] {
			return fmt.Errorf("config: executor_providers lists %q twice", p)
		}
		seen[p] = true
	}

	required := map[string][]string{
		"artifactstore": c.Ports.ArtifactStore.Cmd,
		"generator":     c.Ports.Generator.Cmd,
		"executor":      c.Ports.Executor.Cmd,
		"corpus":        c.Ports.Corpus.Cmd,
		"scorer":        c.Ports.Scorer.Cmd,
	}
	for name, cmd := range required {
		if len(cmd) == 0 {
			return fmt.Errorf("config: ports.%s.cmd is required", name)
		}
	}
	return nil
}
