package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/evol/internal/engine"
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
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	var cfg engine.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return &cfg, nil
}

func runRun(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	artifact, _ := cmd.Flags().GetString("artifact")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format, _ := cmd.Flags().GetString("format")

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
		return emit(cmd, format, map[string]any{
			"dry_run":  true,
			"artifact": cfg.Artifact,
			"plan":     cfg,
		})
	}

	eng := engine.New(*cfg)
	eng.Log = cmd.ErrOrStderr()

	res, err := eng.Run(cmd.Context(), cfg.Artifact)
	switch {
	case err == nil:
		return emit(cmd, format, res)
	case errors.Is(err, engine.ErrNoImprovement):
		if res != nil {
			if emitErr := emit(cmd, format, res); emitErr != nil {
				return emitErr
			}
		}
		return fail(exitNoImprovement, err)
	case errors.Is(err, engine.ErrGate):
		return fail(exitGateFail, err)
	default:
		return fail(exitConfigError, err)
	}
}

func emit(cmd *cobra.Command, format string, payload any) error {
	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	default:
		if res, ok := payload.(*engine.Result); ok {
			printResult(cmd, res)
			return nil
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}
}

func printResult(cmd *cobra.Command, res *engine.Result) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "artifact:  %s (baseline %s @ %.4f)\n",
		res.ArtifactRef, res.BaselineVersion, res.BaselineScore)
	_, _ = fmt.Fprintf(out, "explored:  %d candidate(s) over %d generation(s), best %.4f\n",
		res.CandidatesTried, res.Generations, res.BestScore)
	if res.Accepted {
		_, _ = fmt.Fprintf(out, "promoted:  %s → version %s\n", res.AcceptedID, res.NewVersion)
		return
	}
	_, _ = fmt.Fprintln(out, "promoted:  nothing (no candidate beat the gate)")
}
