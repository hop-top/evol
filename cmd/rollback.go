package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restore an artifact to a previous promoted version",
	Long: `Rollback lists the artifact's version history through the artifact
store and restores a prior version. With no --to it restores the
version immediately before the latest one — undoing the most recent
promotion.

History requires a store with version listing; the reference
filesystem adapter serves it in git-native mode (EVOL_ARTIFACT_GIT=1
with the artifact root inside a git work tree), where every promotion
is a commit and every rollback is a new commit — never a history
rewrite.

Exit codes: 0 restored · 3 configuration or adapter error.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runRollback,
}

func init() {
	rollbackCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	rollbackCmd.Flags().String("artifact", "", "Artifact ref to roll back (required)")
	rollbackCmd.Flags().String("to", "", "Target version (content hash or commit-SHA prefix); default: the version before the latest")
	_ = rollbackCmd.MarkFlagRequired("artifact")
	rootCmd.AddCommand(rollbackCmd)
}

type rollbackResult struct {
	ArtifactRef string `json:"artifact_ref"`
	RestoredTo  string `json:"restored_to"`
	GitCommit   string `json:"git_commit,omitempty"`
}

func runRollback(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	ref, _ := cmd.Flags().GetString("artifact")
	to, _ := cmd.Flags().GetString("to")
	format, _ := cmd.Flags().GetString("format")

	fail := func(err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: exitConfigError, err: err}
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return fail(err)
	}
	if len(cfg.Ports.ArtifactStore.Cmd) == 0 {
		return fail(fmt.Errorf("config: ports.artifactstore.cmd is required"))
	}
	store := cfg.Ports.ArtifactStore.Client("artifactstore")

	var versionsResp struct {
		Versions []struct {
			Version   string `json:"version"`
			GitCommit string `json:"git_commit"`
		} `json:"versions"`
	}
	if err := store.Call(cmd.Context(), "versions",
		map[string]any{"ref": ref}, &versionsResp); err != nil {
		return fail(fmt.Errorf("%w (version history needs a store that serves it — the filesystem adapter does in git-native mode, EVOL_ARTIFACT_GIT=1)", err))
	}
	versions := versionsResp.Versions
	if len(versions) == 0 {
		return fail(fmt.Errorf("no version history for %q", ref))
	}

	target := to
	if target == "" {
		if len(versions) < 2 {
			return fail(fmt.Errorf("%q has only one version — nothing to roll back to", ref))
		}
		target = versions[1].Version
	}

	var restoreResp struct {
		Version   string `json:"version"`
		GitCommit string `json:"git_commit"`
	}
	if err := store.Call(cmd.Context(), "restore",
		map[string]any{"ref": ref, "version": target}, &restoreResp); err != nil {
		return fail(err)
	}

	res := rollbackResult{ArtifactRef: ref, RestoredTo: restoreResp.Version, GitCommit: restoreResp.GitCommit}
	if format == "json" {
		return emit(cmd, format, res)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "rolled back %s to %s", res.ArtifactRef, res.RestoredTo)
	if res.GitCommit != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), " (commit %.12s)", res.GitCommit)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	return nil
}
