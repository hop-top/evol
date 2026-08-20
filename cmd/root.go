package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/evol/internal/version"
	"hop.top/kit/go/console/output"
)

// outputViper carries kit's output flag bindings (--format, --output,
// --cols, --template, ...). Dedicated instance so evol.yaml config keys
// never leak into output resolution.
var outputViper = viper.New()

var rootCmd = &cobra.Command{
	Use:               "evol",
	Short:             "Self-improvement loop for agent capabilities: evaluate, benchmark, replay",
	Version:           version.Version(),
	PersistentPreRunE: resolveFormat,
}

// resolveFormat runs before every verb. Two jobs:
//
//   - default to the bespoke human view when --format was not passed
//     explicitly: kit's flag default is "table", which renders empty
//     for evol's untagged report types;
//   - refuse an unknown --format up front with the usage/config exit
//     code: run can spend minutes before its first render, and silent
//     coercion to the human view would hand a parser garbage, exit 0.
//
// The accepted set is kit's output registry — the same source Dispatch
// consults; there is no parallel format list here.
func resolveFormat(cmd *cobra.Command, _ []string) error {
	pf := cmd.Flags().Lookup("format")
	if pf == nil {
		return nil
	}
	if !pf.Changed {
		outputViper.Set("format", output.Human)
		return nil
	}
	if fh := cmd.Flags().Lookup("format-help"); fh != nil && fh.Changed {
		// Dispatch renders the format catalog; let it handle the key.
		return nil
	}
	if _, ok := output.Default.Lookup(pf.Value.String()); !ok {
		err := fmt.Errorf("unknown output format %q (valid: %s)",
			pf.Value.String(), strings.Join(output.Default.Keys(), ", "))
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
		return &codedError{code: exitConfigError, err: err}
	}
	return nil
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	output.RegisterFlags(rootCmd, outputViper)
	rootCmd.PersistentFlags().BoolP(
		"verbose", "v", false,
		"Verbose output",
	)
}

func initConfig() {
	viper.SetEnvPrefix("evol")
	viper.AutomaticEnv()
	home, err := os.UserHomeDir()
	if err == nil {
		viper.AddConfigPath(
			fmt.Sprintf("%s/.config/evol", home),
		)
	}
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	_ = viper.ReadInConfig()
}
