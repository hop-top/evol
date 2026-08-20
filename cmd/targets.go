package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/evol/internal/engine"
)

var targetsCmd = &cobra.Command{
	Use:   "targets",
	Short: "List artifacts with their evolution history",
	Long: `Targets enumerates every artifact the configured store serves and
joins each with its corpus history: generations recorded, last best
score, last verdict. Rows whose history the corpus cannot serve degrade
to unknowns with a note.

The same rows drive 'evol run --select' when no artifact is given.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runTargets,
}

func init() {
	targetsCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	rootCmd.AddCommand(targetsCmd)
}

func runTargets(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	format, _ := cmd.Flags().GetString("format")

	fail := func(err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: exitConfigError, err: err}
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Normalize(); err != nil {
		return fail(err)
	}

	eng := engine.New(*cfg)
	eng.Log = cmd.ErrOrStderr()

	rows, err := eng.Targets(cmd.Context())
	if err != nil {
		return fail(err)
	}

	if format == "json" {
		return emit(cmd, format, rows)
	}
	printTargets(cmd, rows)
	return nil
}

func printTargets(cmd *cobra.Command, rows []engine.TargetRow) {
	out := cmd.OutOrStdout()
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(out, "no artifacts found")
		return
	}
	_, _ = fmt.Fprintf(out, "%-40s %-12s %4s  %-10s %-10s %s\n",
		"REF", "KIND", "GENS", "LAST BEST", "VERDICT", "STATUS")
	for _, r := range rows {
		score := "-"
		if r.LastBest != nil {
			score = fmt.Sprintf("%.4f", *r.LastBest)
		}
		verdict := r.LastVerdict
		if verdict == "" {
			verdict = "-"
		}
		status := "evolved"
		switch {
		case r.Note != "":
			status = r.Note
		case r.NeverEvolved:
			status = "never evolved"
		}
		_, _ = fmt.Fprintf(out, "%-40s %-12s %4d  %-10s %-10s %s\n",
			truncate(r.Ref, 40), r.Kind, r.Generations, score, verdict, status)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + strings.Repeat("…", 1)
}
