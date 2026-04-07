package cmd

import (
	"github.com/spf13/cobra"
)

var (
	manifestPath string
	modelFilter  []string
	noGPU        bool
	gpuOnly      bool
	sdkVersion   string
	cogVersion   string
	cogBinary    string
	cogRef       string
	sdkWheel     string
	keepImages   bool
)

// NewRootCommand creates the root command
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "test-harness",
		Short: "Go test harness for Cog models",
		Long: `A Go port of the Python test harness for validating cog models.

This tool builds and tests Cog models against specific SDK and CLI versions.
It reads the same manifest.yaml format as the Python version.`,
	}

	// Persistent flags
	rootCmd.PersistentFlags().StringVar(&manifestPath, "manifest", "", "Path to manifest.yaml (default: auto-detect)")
	rootCmd.PersistentFlags().StringArrayVar(&modelFilter, "model", nil, "Run only specific model(s) by name (repeatable)")
	rootCmd.PersistentFlags().BoolVar(&noGPU, "no-gpu", false, "Skip models that require a GPU")
	rootCmd.PersistentFlags().BoolVar(&gpuOnly, "gpu-only", false, "Only run models that require a GPU")
	rootCmd.PersistentFlags().StringVar(&sdkVersion, "sdk-version", "", "Override SDK version")
	rootCmd.PersistentFlags().StringVar(&cogVersion, "cog-version", "", "Override cog CLI version")
	rootCmd.PersistentFlags().StringVar(&cogBinary, "cog-binary", "cog", "Path to cog binary")
	rootCmd.PersistentFlags().StringVar(&cogRef, "cog-ref", "", "Git ref to build cog from")
	rootCmd.PersistentFlags().StringVar(&sdkWheel, "sdk-wheel", "", "Path to pre-built SDK wheel")
	rootCmd.PersistentFlags().BoolVar(&keepImages, "keep-images", false, "Don't clean up Docker images")

	// Subcommands
	rootCmd.AddCommand(newRunCommand())
	rootCmd.AddCommand(newBuildCommand())
	rootCmd.AddCommand(newListCommand())
	rootCmd.AddCommand(newSchemaCompareCommand())

	return rootCmd
}
