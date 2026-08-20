package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"hop.top/evol/internal/engine"
)

var runsCmd = &cobra.Command{
	Use:   "runs",
	Short: "Operator-facing run ledger",
	Long: `Runs reads the loop's audit ledger through the Audit port
(spec/port-audit.md): one entry per run — outcome, headline metrics,
one step per generation. Bind ports.audit to audit-tlc to keep the
ledger in the family tracker, or audit-fs for a plain JSONL file.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var runsListCmd = &cobra.Command{
	Use:           "list",
	Short:         "List recorded runs, newest first",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runRunsList,
}

var runsShowCmd = &cobra.Command{
	Use:           "show <run-id>",
	Short:         "Show one recorded run, steps included",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runRunsShow,
}

func init() {
	runsCmd.PersistentFlags().String("config", "evol.yaml", "Loop configuration file")
	runsListCmd.Flags().String("subject", "", "Filter by subject (artifact ref)")
	runsListCmd.Flags().Int("limit", 0, "Maximum rows (0 = all)")
	runsCmd.AddCommand(runsListCmd)
	runsCmd.AddCommand(runsShowCmd)
	rootCmd.AddCommand(runsCmd)
}

func auditEngine(cmd *cobra.Command) (*engine.Engine, error) {
	configPath, _ := cmd.Flags().GetString("config")
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	// Deliberately no full Normalize(): the runs ledger is read-only —
	// demanding an executor/generator/corpus config to list past runs
	// would be hostile. Only the audit port matters here.
	eng := engine.New(*cfg)
	eng.Log = cmd.ErrOrStderr()
	return eng, nil
}

func auditFail(cmd *cobra.Command, err error) error {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
	return &codedError{code: exitConfigError, err: err}
}

func runRunsList(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	subject, _ := cmd.Flags().GetString("subject")
	limit, _ := cmd.Flags().GetInt("limit")

	eng, err := auditEngine(cmd)
	if err != nil {
		return auditFail(cmd, err)
	}
	runs, err := eng.AuditList(cmd.Context(), subject, limit)
	if err != nil {
		return auditFail(cmd, err)
	}
	if format == "json" {
		return emit(cmd, format, runs)
	}
	printRuns(cmd, runs)
	return nil
}

func runRunsShow(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")

	eng, err := auditEngine(cmd)
	if err != nil {
		return auditFail(cmd, err)
	}
	run, err := eng.AuditShow(cmd.Context(), args[0])
	if err != nil {
		return auditFail(cmd, err)
	}
	if format == "json" {
		return emit(cmd, format, run)
	}
	printRun(cmd, run)
	return nil
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return "-"
}

func printRuns(cmd *cobra.Command, runs []map[string]any) {
	out := cmd.OutOrStdout()
	if len(runs) == 0 {
		_, _ = fmt.Fprintln(out, "no runs recorded")
		return
	}
	_, _ = fmt.Fprintf(out, "%-38s %-32s %-16s %-10s %s\n",
		"RUN-ID", "SUBJECT", "OUTCOME", "BEST", "STARTED")
	for _, r := range runs {
		best := "-"
		if m, ok := r["metrics"].(map[string]any); ok {
			if v, ok := m["best_score"].(float64); ok {
				best = fmt.Sprintf("%.4f", v)
			}
		}
		_, _ = fmt.Fprintf(out, "%-38s %-32s %-16s %-10s %s\n",
			str(r, "run_id"), str(r, "subject"), str(r, "outcome"),
			best, str(r, "started_at"))
	}
}

func printRun(cmd *cobra.Command, run map[string]any) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "run:      %s\n", str(run, "run_id"))
	_, _ = fmt.Fprintf(out, "subject:  %s\n", str(run, "subject"))
	_, _ = fmt.Fprintf(out, "outcome:  %s\n", str(run, "outcome"))
	_, _ = fmt.Fprintf(out, "window:   %s → %s\n",
		str(run, "started_at"), str(run, "finished_at"))
	if metrics, ok := run["metrics"].(map[string]any); ok && len(metrics) > 0 {
		_, _ = fmt.Fprintln(out, "metrics:")
		for _, k := range []string{"baseline_score", "best_score", "sig_p", "generations", "candidates_tried"} {
			if v, ok := metrics[k]; ok {
				_, _ = fmt.Fprintf(out, "  %-18s %v\n", k, v)
			}
		}
	}
	steps, ok := run["steps"].([]any)
	if !ok || len(steps) == 0 {
		return
	}
	_, _ = fmt.Fprintln(out, "steps:")
	for _, s := range steps {
		step, ok := s.(map[string]any)
		if !ok {
			continue
		}
		detail := str(step, "detail")
		if detail == "-" {
			detail = ""
		}
		_, _ = fmt.Fprintf(out, "  %-14s %-10s %s\n",
			str(step, "name"), str(step, "status"), detail)
	}
}
