package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"hop.top/evol/internal/port"
)

var routingCmd = &cobra.Command{
	Use:   "routing",
	Short: "Turn evaluation evidence into routing configuration",
}

var routingEmitCmd = &cobra.Command{
	Use:   "emit",
	Short: "Emit a pool config from provider-attributed corpus evidence",
	Long: `Emit aggregates every provider-attributed score row the corpus holds
for an artifact — sweep evidence and gated rows alike — into per-provider
means, and hands them to the routing adapter, which writes a kit-llm pool
fragment. Per-model evaluation results become routing weights; the pool
file is itself a tool-config artifact the loop can evolve.

v0 reads the corpus store directly (the corpus port exposes no evidence
action yet — see docs/routing-writeback.md).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runRoutingEmit,
}

func init() {
	routingEmitCmd.Flags().String("config", "evol.yaml", "Loop configuration file")
	routingEmitCmd.Flags().String("artifact", "", "Artifact ref whose evidence to aggregate (required)")
	routingEmitCmd.Flags().String("out", ".evol/routing-pool.yaml", "Pool fragment output path")
	routingEmitCmd.Flags().String("adapter", "", "Routing adapter argv override (JSON array; default [\"routing-emit\"] or EVOL_ROUTING_CMD)")
	routingCmd.AddCommand(routingEmitCmd)
	rootCmd.AddCommand(routingCmd)
}

// providerStat aggregates one provider's rows.
type providerStat struct {
	Provider  string  `json:"provider"`
	MeanScore float64 `json:"mean_score"`
	N         int     `json:"n"`
}

func runRoutingEmit(cmd *cobra.Command, _ []string) error {
	fail := func(err error) error {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: exitConfigError, err: err}
	}

	artifact, _ := cmd.Flags().GetString("artifact")
	if artifact == "" {
		return fail(errors.New("--artifact is required"))
	}
	outPath, _ := cmd.Flags().GetString("out")

	root := os.Getenv("EVOL_CORPUS_ROOT")
	if root == "" {
		return fail(errors.New("EVOL_CORPUS_ROOT is not set (same contract as the corpus adapter)"))
	}
	stats, err := aggregateEvidence(root, artifact, cmd.ErrOrStderr())
	if err != nil {
		return fail(err)
	}
	if len(stats) == 0 {
		return fail(errors.New("no provider-attributed rows in the corpus for this artifact; run with executor_provider(s) configured first"))
	}

	argv, err := routingArgv(cmd)
	if err != nil {
		return fail(err)
	}
	client := &port.Client{Port: "routing", Cmd: argv}
	var resp struct {
		Written string `json:"written"`
		Entries []struct {
			Alias  string  `json:"alias"`
			Model  string  `json:"model"`
			Weight float64 `json:"weight"`
		} `json:"entries"`
	}
	err = client.Call(cmd.Context(), "emit", map[string]any{
		"artifact_ref": artifact,
		"evidence":     stats,
		"output":       map[string]any{"path": outPath, "format": "kit-llm-pool"},
	}, &resp)
	if err != nil {
		return fail(err)
	}

	format, _ := cmd.Flags().GetString("format")
	if format == "json" {
		return emit(cmd, format, resp)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pool written: %s (%d entries)\n", resp.Written, len(resp.Entries))
	for _, e := range resp.Entries {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %-24s %-28s %.2f\n", e.Alias, e.Model, e.Weight)
	}
	return nil
}

func routingArgv(cmd *cobra.Command) ([]string, error) {
	if raw, _ := cmd.Flags().GetString("adapter"); raw != "" {
		return parseArgv(raw)
	}
	if raw := os.Getenv("EVOL_ROUTING_CMD"); raw != "" {
		return parseArgv(raw)
	}
	return []string{"routing-emit"}, nil
}

func parseArgv(raw string) ([]string, error) {
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil || len(argv) == 0 {
		return nil, fmt.Errorf("adapter argv %q is not a non-empty JSON array", raw)
	}
	return argv, nil
}

// aggregateEvidence reads the corpus-fs store directly (layout:
// <root>/<sha256(ref)[:12]>/generations.jsonl) and averages row means
// per provider across every provider-attributed row — sweep evidence
// and gated rows alike; the primary provider's gated rows are its
// evidence.
func aggregateEvidence(root, artifactRef string, warn interface{ Write([]byte) (int, error) }) ([]providerStat, error) {
	sum := sha256.Sum256([]byte(artifactRef))
	path := filepath.Join(root, hex.EncodeToString(sum[:])[:12], "generations.jsonl")
	f, err := os.Open(path) //nolint:gosec // path derives from EVOL_CORPUS_ROOT by design
	if err != nil {
		return nil, fmt.Errorf("open corpus store %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	type row struct {
		Provider string `json:"provider"`
		Scores   []struct {
			Score float64 `json:"score"`
		} `json:"scores"`
	}
	agg := map[string]*providerStat{}
	order := []string{}
	dec := json.NewDecoder(f)
	malformed := 0
	for dec.More() {
		var r row
		if err := dec.Decode(&r); err != nil {
			malformed++
			// Resync: a broken line poisons the stream decoder; bail with
			// what we have rather than loop forever.
			break
		}
		if r.Provider == "" || len(r.Scores) == 0 {
			continue
		}
		total := 0.0
		for _, s := range r.Scores {
			total += s.Score
		}
		m := total / float64(len(r.Scores))
		st, ok := agg[r.Provider]
		if !ok {
			st = &providerStat{Provider: r.Provider}
			agg[r.Provider] = st
			order = append(order, r.Provider)
		}
		// Running mean of row means.
		st.MeanScore = (st.MeanScore*float64(st.N) + m) / float64(st.N+1)
		st.N++
	}
	if malformed > 0 {
		_, _ = fmt.Fprintf(warn, "evol: %d malformed corpus line(s) skipped\n", malformed)
	}
	stats := make([]providerStat, 0, len(agg))
	for _, p := range order {
		stats = append(stats, *agg[p])
	}
	return stats, nil
}
