package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/evol/internal/engine"
)

var casesCmd = &cobra.Command{
	Use:   "cases",
	Short: "Manage eval cases (synthesize, promote)",
}

var synthCmd = &cobra.Command{
	Use:   "synth",
	Short: "Synthesize eval cases grounded in the knowledge base",
	Long: `Synth generates new eval cases through the casegen port, grounded
in knowledgebase passages for the artifact. Generated cases always land
QUARANTINED with provenance "synthetic": they join the eval pool only
after review, via "evol cases promote".

Exit codes: 0 cases synthesized (possibly zero) · 2 no grounding
knowledge available · 3 configuration or adapter error.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runSynth,
}

var promoteCmd = &cobra.Command{
	Use:   "promote",
	Short: "Promote reviewed quarantined cases into the eval pool",
	Long: `Promote clears quarantine on reviewed cases so they join the eval
pool served by the corpus "cases" action.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runPromote,
}

func init() {
	synthCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	synthCmd.Flags().String("artifact", "", "Artifact ref (overrides config)")
	synthCmd.Flags().Int("count", 5, "Cases to request from the casegen port")
	promoteCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	promoteCmd.Flags().String("artifact", "", "Artifact ref (overrides config)")
	promoteCmd.Flags().String("ids", "", "Comma-separated case ids to promote (required)")
	casesCmd.AddCommand(synthCmd)
	casesCmd.AddCommand(promoteCmd)
	rootCmd.AddCommand(casesCmd)
}

// synthEngine loads config + resolves the artifact ref for the cases
// subcommands, which share flag conventions with run.
func synthEngine(cmd *cobra.Command) (*engine.Engine, string, error) {
	configPath, _ := cmd.Flags().GetString("config")
	artifact, _ := cmd.Flags().GetString("artifact")

	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, "", &codedError{code: exitConfigError, err: err}
	}
	if artifact != "" {
		cfg.Artifact = artifact
	}
	if err := cfg.Normalize(); err != nil {
		return nil, "", &codedError{code: exitConfigError, err: err}
	}
	if cfg.Artifact == "" {
		return nil, "", &codedError{code: exitConfigError,
			err: errors.New("an artifact ref is required (--artifact or config)")}
	}
	eng := engine.New(*cfg)
	eng.Log = cmd.ErrOrStderr()
	return eng, cfg.Artifact, nil
}

func runSynth(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	count, _ := cmd.Flags().GetInt("count")

	fail := func(code int, err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: code, err: err}
	}

	eng, artifact, err := synthEngine(cmd)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return err
	}

	res, err := eng.SynthesizeCases(cmd.Context(), artifact, count)
	switch {
	case err == nil:
		if err := emit(cmd, format, res); err != nil {
			return err
		}
		if res.Added > 0 {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"review the quarantined cases, then: evol cases promote --artifact %s --ids <id,...>\n",
				artifact)
		}
		return nil
	case errors.Is(err, engine.ErrNoKnowledge):
		return fail(exitGateFail, err)
	default:
		return fail(exitConfigError, err)
	}
}

func runPromote(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	rawIDs, _ := cmd.Flags().GetString("ids")

	fail := func(code int, err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: code, err: err}
	}

	ids := make([]string, 0, 4)
	for _, id := range strings.Split(rawIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return fail(exitConfigError, errors.New("--ids is required"))
	}

	eng, artifact, err := synthEngine(cmd)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return err
	}

	promoted, missing, err := eng.PromoteCases(cmd.Context(), artifact, ids)
	if err != nil {
		return fail(exitConfigError, err)
	}
	return emit(cmd, format, map[string]any{
		"artifact_ref": artifact,
		"promoted":     promoted,
		"missing":      missing,
	})
}
