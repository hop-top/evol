package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/evol/internal/port"
)

// casesListCmd is the review surface for the eval pool: it can show
// the gating pool, the quarantined intake awaiting review, or both.
var casesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List eval cases (review surface for the quarantine queue)",
	Long: `List shows an artifact's eval cases through the corpus port.

By default only the gating pool is shown (what "cases" serves to a
run). --quarantined shows the intake awaiting review; --all shows both.

Exit codes: 0 listed · 3 configuration or adapter error.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runCasesList,
}

// casesCorrectCmd is the human correction write path: corrections land
// unquarantined (they are human-authored) and are merged into the
// gating pool at the next eval-set build.
var casesCorrectCmd = &cobra.Command{
	Use:   "correct",
	Short: "Record a human correction as an eval case",
	Long: `Correct records a human-authored case through the corpus
"add-corrections" action. Corrections are NOT quarantined: the engine
merges them into the gating pool at the next eval-set build.

Exit codes: 0 recorded (or duplicate) · 3 configuration or adapter error.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runCasesCorrect,
}

func init() {
	casesListCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	casesListCmd.Flags().String("artifact", "", "Artifact ref (overrides config)")
	casesListCmd.Flags().Bool("quarantined", false, "Show only quarantined cases awaiting review")
	casesListCmd.Flags().Bool("all", false, "Show gating pool and quarantined cases")

	casesCorrectCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	casesCorrectCmd.Flags().String("artifact", "", "Artifact ref (overrides config)")
	casesCorrectCmd.Flags().String("case-id", "", "Correction case id (required)")
	casesCorrectCmd.Flags().String("input", "", "Case input (required)")
	casesCorrectCmd.Flags().String("expected", "", "Expected behavior/output")
	casesCorrectCmd.Flags().String("split", "train", "Split the correction joins (train or holdout)")

	casesCmd.AddCommand(casesListCmd)
	casesCmd.AddCommand(casesCorrectCmd)
}

// reviewCase mirrors the corpus case entry shape for review output.
type reviewCase struct {
	ID          string `json:"id"`
	Input       string `json:"input"`
	Expected    string `json:"expected,omitempty"`
	Split       string `json:"split,omitempty"`
	Source      string `json:"source,omitempty"`
	Quarantined bool   `json:"quarantined,omitempty"`
}

// corpusClient resolves the corpus port from the run configuration so
// the review verbs speak the same contract as the engine.
func corpusClient(cmd *cobra.Command) (*port.Client, string, error) {
	configPath, _ := cmd.Flags().GetString("config")
	artifact, _ := cmd.Flags().GetString("artifact")

	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, "", &codedError{code: exitConfigError, err: err}
	}
	if artifact != "" {
		cfg.Artifact = artifact
	}
	// Deliberately no full Normalize(): review verbs need only the
	// corpus port — demanding an executor/generator config to list
	// cases would be hostile.
	if cfg.Artifact == "" {
		return nil, "", &codedError{code: exitConfigError,
			err: errors.New("an artifact ref is required (--artifact or config)")}
	}
	client := cfg.Ports.Corpus.Client("corpus")
	if !client.Configured() {
		return nil, "", &codedError{code: exitConfigError,
			err: errors.New("no corpus port configured (ports.corpus.cmd)")}
	}
	return client, cfg.Artifact, nil
}

func runCasesList(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	onlyQuarantined, _ := cmd.Flags().GetBool("quarantined")
	all, _ := cmd.Flags().GetBool("all")

	fail := func(err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: exitConfigError, err: err}
	}

	client, artifact, err := corpusClient(cmd)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return err
	}

	var resp struct {
		Cases []reviewCase `json:"cases"`
	}
	params := map[string]any{"artifact_ref": artifact}
	if onlyQuarantined || all {
		params["include_quarantined"] = true
	}
	if err := client.Call(cmd.Context(), "cases", params, &resp); err != nil {
		return fail(err)
	}

	cases := resp.Cases
	if onlyQuarantined {
		kept := cases[:0]
		for _, c := range cases {
			if c.Quarantined {
				kept = append(kept, c)
			}
		}
		cases = kept
	}

	if format == "json" {
		return emit(cmd, format, map[string]any{
			"artifact_ref": artifact,
			"cases":        cases,
		})
	}

	if len(cases) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no cases")
		return nil
	}
	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "%-22s %-8s %-10s %-5s %s\n", "ID", "SPLIT", "SOURCE", "QUAR", "INPUT")
	for _, c := range cases {
		quar := "-"
		if c.Quarantined {
			quar = "yes"
		}
		_, _ = fmt.Fprintf(w, "%-22s %-8s %-10s %-5s %s\n",
			c.ID, orDash(c.Split), orDash(c.Source), quar, truncateCase(c.Input, 60))
	}
	return nil
}

func runCasesCorrect(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	caseID, _ := cmd.Flags().GetString("case-id")
	input, _ := cmd.Flags().GetString("input")
	expected, _ := cmd.Flags().GetString("expected")
	split, _ := cmd.Flags().GetString("split")

	fail := func(err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: exitConfigError, err: err}
	}

	if strings.TrimSpace(caseID) == "" {
		return fail(errors.New("--case-id is required"))
	}
	if strings.TrimSpace(input) == "" {
		return fail(errors.New("--input is required"))
	}
	if split != "train" && split != "holdout" {
		return fail(fmt.Errorf("--split must be train or holdout, got %q", split))
	}

	client, artifact, err := corpusClient(cmd)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return err
	}

	var resp struct {
		Added      int      `json:"added"`
		Duplicates int      `json:"duplicates"`
		IDs        []string `json:"ids"`
	}
	err = client.Call(cmd.Context(), "add-corrections", map[string]any{
		"artifact_ref": artifact,
		"corrections": []map[string]any{{
			"id":       caseID,
			"input":    input,
			"expected": expected,
			"split":    split,
		}},
	}, &resp)
	if err != nil {
		return fail(err)
	}

	if err := emit(cmd, format, map[string]any{
		"artifact_ref": artifact,
		"added":        resp.Added,
		"duplicates":   resp.Duplicates,
		"ids":          resp.IDs,
	}); err != nil {
		return err
	}
	if resp.Added > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"correction recorded; it joins the %q pool at the next eval-set build\n", split)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func truncateCase(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
