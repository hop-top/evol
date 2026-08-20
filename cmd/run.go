package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"hop.top/evol/internal/engine"
	kitconfig "hop.top/kit/go/core/config"
)

// Exit codes for `evol run`.
const (
	exitPromoted      = 0
	exitNoImprovement = 1
	exitGateFail      = 2
	exitConfigError   = 3
)

// codedError carries a process exit code through cobra's error path.
type codedError struct {
	code int
	err  error
}

func (c *codedError) Error() string { return c.err.Error() }
func (c *codedError) Unwrap() error { return c.err }

// ExitCode maps an error returned by Execute to a process exit code.
func ExitCode(err error) int {
	var coded *codedError
	if errors.As(err, &coded) {
		return coded.code
	}
	return 1
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the self-improvement loop for one artifact",
	Long: `Run loads the artifact, evaluates a baseline over the holdout
cases, then proposes, executes, scores, and gates candidate revisions.
An accepted candidate is written back through the artifact store; every
candidate and verdict is recorded to the corpus either way.

Exit codes: 0 candidate promoted · 1 no improvement · 2 gate or
precondition failure · 3 configuration or adapter error.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runRun,
}

func init() {
	runCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	runCmd.Flags().String("artifact", "", "Artifact ref to evolve (overrides config)")
	runCmd.Flags().String("select", engine.SelectNeverRun,
		"Target selection policy when no artifact is given (never-run|worst|stale|drift|kb-churn)")
	runCmd.Flags().Bool("dry-run", false, "Print the resolved plan without spawning adapters")
	rootCmd.AddCommand(runCmd)
}

func loadConfig(path string) (*engine.Config, error) {
	// kit's layered loader is yaml.v3-based, so map keys (notably
	// ports.<name>.env variable names) keep their case — viper
	// lowercases them.
	var cfg engine.Config
	if err := kitconfig.Load(&cfg, kitconfig.Options{
		ExtraConfigPaths: []string{path},
	}); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return &cfg, nil
}

func runRun(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	artifact, _ := cmd.Flags().GetString("artifact")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	fail := func(code int, err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: code, err: err}
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return fail(exitConfigError, err)
	}
	if artifact != "" {
		cfg.Artifact = artifact
	}
	if err := cfg.Normalize(); err != nil {
		return fail(exitConfigError, err)
	}
	// Explicit ref (flag, then config) has absolute precedence; without
	// one, a selection policy picks the target from the store × corpus.
	if cfg.Artifact == "" {
		policy, _ := cmd.Flags().GetString("select")
		eng := engine.New(*cfg)
		eng.Log = cmd.ErrOrStderr()
		rows, err := eng.Targets(cmd.Context())
		if err != nil {
			return fail(exitConfigError, err)
		}
		ref, err := engine.SelectTarget(rows, policy)
		if err != nil {
			return fail(exitConfigError, err)
		}
		cfg.Artifact = ref
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"selected %s (policy %s)\n", ref, policy)
	}

	if dryRun {
		return render(cmd, dryRunPlan{
			Artifact: cfg.Artifact,
			DryRun:   true,
			Plan:     cfg,
		})
	}

	eng := engine.New(*cfg)
	eng.Log = cmd.ErrOrStderr()

	res, err := eng.Run(cmd.Context(), cfg.Artifact)
	switch {
	case err == nil:
		runPromotionHook(cmd, cfg, res)
		return render(cmd, (*runResult)(res))
	case errors.Is(err, engine.ErrNoImprovement):
		if res != nil {
			if renderErr := render(cmd, (*runResult)(res)); renderErr != nil {
				return renderErr
			}
		}
		return fail(exitNoImprovement, err)
	case errors.Is(err, engine.ErrGate):
		return fail(exitGateFail, err)
	default:
		return fail(exitConfigError, err)
	}
}

// runResult renders *engine.Result through kit's output dispatch:
// JSON/YAML from the engine's json tags, human via the run summary.
type runResult engine.Result

func (r *runResult) MarshalYAML() (any, error) { return jsonForm(r) }

func (r *runResult) RenderHuman(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "artifact:  %s (baseline %s @ %.4f)\n",
		r.ArtifactRef, r.BaselineVersion, r.BaselineScore)
	_, _ = fmt.Fprintf(w, "explored:  %d candidate(s) over %d generation(s), best %.4f\n",
		r.CandidatesTried, r.Generations, r.BestScore)
	if r.Accepted {
		_, _ = fmt.Fprintf(w, "promoted:  %s → version %s\n", r.AcceptedID, r.NewVersion)
		return nil
	}
	_, _ = fmt.Fprintln(w, "promoted:  nothing (no candidate beat the gate)")
	return nil
}

// dryRunPlan mirrors the retired map payload; field order preserves
// the alphabetical key order maps marshaled with, keeping --dry-run
// JSON byte-stable for story replays.
type dryRunPlan struct {
	Artifact string         `json:"artifact"`
	DryRun   bool           `json:"dry_run"`
	Plan     *engine.Config `json:"plan"`
}

func (d dryRunPlan) MarshalYAML() (any, error)     { return jsonForm(d) }
func (d dryRunPlan) RenderHuman(w io.Writer) error { return renderHumanJSON(w, d) }
