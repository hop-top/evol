package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/evol/internal/version"
)

var rootCmd = &cobra.Command{
	Use:     "evol",
	Short:   "Self-improvement loop for agent capabilities: evaluate, benchmark, replay",
	Version: version.Version(),
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringP(
		"format", "f", "text",
		"Output format (text, json, yaml)",
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
