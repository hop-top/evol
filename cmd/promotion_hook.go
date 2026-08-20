package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"hop.top/evol/internal/engine"
)

// runPromotionHook executes the operator-configured promotion.hook argv
// after a successful promotion. Hook placement is deliberately in the
// CLI's post-run path, not the engine: the engine's contract ends at
// "artifact written, corpus recorded"; what an operator does with a
// promotion (publish a package, install a capability, notify) is
// operator policy. A failing hook warns and never fails the promotion —
// the improvement is already real. Repository release processes are a
// separate, explicitly operator-gated concern; this hook runs exactly
// the argv configured, nothing more.
func runPromotionHook(cmd *cobra.Command, cfg *engine.Config, res *engine.Result) {
	if res == nil || !res.Accepted || len(cfg.Promotion.Hook) == 0 {
		return
	}
	argv := cfg.Promotion.Hook
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	hook := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- operator-configured argv, that is the feature
	hook.Env = append(os.Environ(),
		"EVOL_PROMOTED_REF="+res.ArtifactRef,
		"EVOL_PROMOTED_VERSION="+res.NewVersion,
		"EVOL_PROMOTED_GIT_COMMIT="+res.GitCommit,
	)
	hook.Stdout = cmd.ErrOrStderr()
	hook.Stderr = cmd.ErrOrStderr()
	if err := hook.Run(); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"evol: warning: promotion hook failed (promotion stands): %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "promotion hook ran for %s@%s\n",
		res.ArtifactRef, res.NewVersion)
}
