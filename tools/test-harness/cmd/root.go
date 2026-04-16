package cmd

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

// validateConcurrency checks that the concurrency flag is a valid value.
// errgroup.SetLimit panics on 0, and negative values mean unlimited.
func validateConcurrency() error {
	if concurrency < 1 {
		return fmt.Errorf("--concurrency must be at least 1, got %d", concurrency)
	}
	return nil
}

// modelAction is a function that processes a single model and returns a result.
type modelAction[T any] func(ctx context.Context, model manifest.Model) *T

// statusPrinter formats a per-model status line after processing completes.
type statusPrinter[T any] func(index, total int, model manifest.Model, result *T) string

// runModels executes an action for each model, either sequentially or in parallel
// depending on the concurrency setting. It handles the common pattern of:
//   - printing a "starting" line
//   - running the action
//   - printing a "done/failed" status line
//
// The results slice is pre-allocated by the caller. This function fills it in.
func runModels[T any](
	ctx context.Context,
	models []manifest.Model,
	results []T,
	parallel bool,
	action modelAction[T],
	startLine func(index, total int, model manifest.Model) string,
	statusLine statusPrinter[T],
) {
	if parallel {
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		var mu sync.Mutex

		for i, model := range models {
			wg.Go(func() {
				sem <- struct{}{}        // acquire
				defer func() { <-sem }() // release

				mu.Lock()
				fmt.Print(startLine(i+1, len(models), model))
				mu.Unlock()

				result := action(ctx, model)
				results[i] = *result

				mu.Lock()
				fmt.Print(statusLine(i+1, len(models), model, result))
				mu.Unlock()
			})
		}
		wg.Wait()
	} else {
		for i, model := range models {
			fmt.Print(startLine(i+1, len(models), model))
			result := action(ctx, model)
			results[i] = *result
			fmt.Print(statusLine(i+1, len(models), model, result))
		}
	}
}

// formatFailureSummary builds an error message with per-model failure details.
//
//nolint:gosec // G705: writes to strings.Builder, not an HTTP response — no XSS risk
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
