package main

import (
	"os"

	"hop.top/evol/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
