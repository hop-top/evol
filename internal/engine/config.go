package engine

import (
	"fmt"
	"time"

	"hop.top/evol/internal/port"
)

// PortConfig binds one port to an adapter command.
type PortConfig struct {
	// Cmd is the adapter argv; Cmd[0] is the executable.
	Cmd []string `mapstructure:"cmd" json:"cmd"`
	// TimeoutSeconds bounds each call; 0 uses the port default.
	TimeoutSeconds int `mapstructure:"timeout_seconds" json:"timeout_seconds,omitempty"`
}

func (p PortConfig) client(name string) *port.Client {
	return &port.Client{
		Port:    name,
		Cmd:     p.Cmd,
		Timeout: time.Duration(p.TimeoutSeconds) * time.Second,
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
		// Scorer is an engine-level draft contract; see
		// docs/scorer-draft.md.
		Scorer PortConfig `mapstructure:"scorer" json:"scorer"`
		// KnowledgeBase is optional; leave cmd empty to disable.
		KnowledgeBase PortConfig `mapstructure:"knowledgebase" json:"knowledgebase"`
	} `mapstructure:"ports" json:"ports"`

	Thresholds struct {
		// Delta is the required improvement over the baseline mean.
		Delta float64 `mapstructure:"delta" json:"delta"`
		// Trials is how many times each case is run and scored;
		// scores average across trials. Minimum 1.
		Trials int `mapstructure:"trials" json:"trials"`
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
