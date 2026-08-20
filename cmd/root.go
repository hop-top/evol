package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/evol/internal/version"
)

// Accepted --format values. Every verb renders through emit, so this
// registry is the single source of truth for the flag's contract: the
// help string derives from it and validateFormat enforces it.
const (
	formatText = "text"
	formatJSON = "json"
	formatYAML = "yaml"
)

var acceptedFormats = []string{formatText, formatJSON, formatYAML}

var rootCmd = &cobra.Command{
	Use:     "evol",
	Short:   "Self-improvement loop for agent capabilities: evaluate, benchmark, replay",
	Version: version.Version(),
	// Runs before every verb: an unknown --format must refuse loudly,
	// not silently coerce to the text table — a caller parsing stdout
	// as its requested format would read garbage with exit 0.
	PersistentPreRunE: validateFormat,
}

func Execute() error {
	return rootCmd.Execute()
}

func validateFormat(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("format")
	if slices.Contains(acceptedFormats, format) {
		return nil
	}
	err := fmt.Errorf("unknown output format %q (valid: %s)",
		format, strings.Join(acceptedFormats, ", "))
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "evol: %v\n", err)
	return &codedError{code: exitConfigError, err: err}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringP(
		"format", "f", formatText,
		"Output format ("+strings.Join(acceptedFormats, ", ")+")",
	)
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
