package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
	"github.com/replicate/cog/tools/test-harness/internal/report"
	"github.com/replicate/cog/tools/test-harness/internal/resolver"
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
	cleanImages  bool
	keepOutputs  bool
	concurrency  int
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
	rootCmd.PersistentFlags().BoolVar(&cleanImages, "clean-images", false, "Remove Docker images after run (default: keep them)")
	rootCmd.PersistentFlags().BoolVar(&keepOutputs, "keep-outputs", false, "Preserve prediction outputs (images, files) in the work directory")
	rootCmd.PersistentFlags().IntVar(&concurrency, "concurrency", 4, "Maximum number of models to build/test in parallel")

	// Subcommands
	rootCmd.AddCommand(newRunCommand())
	rootCmd.AddCommand(newBuildCommand())
	rootCmd.AddCommand(newListCommand())
	rootCmd.AddCommand(newSchemaCompareCommand())

	return rootCmd
}

// resolveSetup loads the manifest, resolves versions, and filters models.
func resolveSetup() (*manifest.Manifest, []manifest.Model, *resolver.Result, error) {
	mf, mfPath, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loading manifest: %w", err)
	}
	fmt.Printf("Loaded manifest: %s\n", mfPath)

	fmt.Println("Resolving versions...")
	resolved, err := resolver.Resolve(cogBinary, cogVersion, cogRef, sdkVersion, sdkWheel, map[string]string{
		"sdk_version": mf.Defaults.SDKVersion,
		"cog_version": mf.Defaults.CogVersion,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolving versions: %w", err)
	}
	fmt.Printf("Using cog CLI: %s (%s)\n", resolved.CogBinary, resolved.CogVersion)

	models := mf.FilterModels(modelFilter, noGPU, gpuOnly)
	return mf, models, resolved, nil
}

// formatFailureSummary builds an error message with per-model failure details.
func formatFailureSummary(action string, results []report.ModelResult) error {
	var b strings.Builder
	var failCount int
	for _, r := range results {
		if r.Passed || r.Skipped {
			continue
		}
		failCount++
		fmt.Fprintf(&b, "\n  x %s", r.Name)
		if r.Error != "" {
			// Show first line of the error
			firstLine := r.Error
			if idx := strings.Index(firstLine, "\n"); idx != -1 {
				firstLine = firstLine[:idx]
			}
			fmt.Fprintf(&b, ": %s", firstLine)
		} else {
			// Summarize failed tests
			for _, t := range r.TestResults {
				if !t.Passed {
					msg := t.Message
					if idx := strings.Index(msg, "\n"); idx != -1 {
						msg = msg[:idx]
					}
					fmt.Fprintf(&b, "\n      test %q: %s", t.Description, msg)
				}
			}
			for _, t := range r.TrainResults {
				if !t.Passed {
					msg := t.Message
					if idx := strings.Index(msg, "\n"); idx != -1 {
						msg = msg[:idx]
					}
					fmt.Fprintf(&b, "\n      train %q: %s", t.Description, msg)
				}
			}
		}
	}
	return fmt.Errorf("%d %s(s) failed:%s", failCount, action, b.String())
}
