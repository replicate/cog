package main

import (
	"os"

	"github.com/replicate/cog/tools/test-harness/cmd"
)

func main() {
	rootCmd := cmd.NewRootCommand()
	rootCmd.SilenceErrors = true
	if err := rootCmd.Execute(); err != nil {
		// Cobra's SilenceErrors prevents double-printing.
		// We print manually to control the format.
		rootCmd.PrintErrln("Error:", err)
		os.Exit(1)
	}
}
