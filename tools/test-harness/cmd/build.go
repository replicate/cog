package cmd

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/tools/test-harness/internal/report"
	"github.com/replicate/cog/tools/test-harness/internal/runner"
)

func newBuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Build model images only",
		Long:  `Build Docker images for all models in the manifest without running predictions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd.Context())
		},
	}
}

func runBuild(ctx context.Context) error {
	_, models, resolved, err := resolveSetup()
	if err != nil {
		return err
	}

	if len(models) == 0 {
		fmt.Println("No models to build")
		return nil
	}

	parallel := concurrency > 1 && len(models) > 1
	if parallel {
		fmt.Printf("Building %d model(s) with concurrency %d\n\n", len(models), concurrency)
	} else {
		fmt.Printf("Building %d model(s)\n\n", len(models))
	}

	// Create runner
	r, err := runner.New(runner.Options{
		CogBinary:   resolved.CogBinary,
		SDKVersion:  resolved.SDKPatchVersion,
		SDKWheel:    resolved.SDKWheel,
		CleanImages: cleanImages,
		KeepOutputs: keepOutputs,
		Quiet:       parallel,
	})
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}
	defer func() { _ = r.Cleanup() }()

	// Build models
	results := make([]report.ModelResult, len(models))

	if parallel {
		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(concurrency)

		var mu sync.Mutex
		for i, model := range models {
			g.Go(func() error {
				mu.Lock()
				fmt.Printf("  [%d/%d] Building %s...\n", i+1, len(models), model.Name)
				mu.Unlock()

				result := r.BuildModel(ctx, model)
				results[i] = *result

				mu.Lock()
				switch {
				case result.Passed:
					fmt.Printf("  [%d/%d] + %s (%.1fs)\n", i+1, len(models), model.Name, result.BuildDuration)
				case result.Skipped:
					fmt.Printf("  [%d/%d] - %s (skipped: %s)\n", i+1, len(models), model.Name, result.SkipReason)
				default:
					fmt.Printf("  [%d/%d] x %s FAILED\n", i+1, len(models), model.Name)
				}
				mu.Unlock()

				return nil
			})
		}
		_ = g.Wait()
	} else {
		for i, model := range models {
			fmt.Printf("Building %s...\n", model.Name)
			result := r.BuildModel(ctx, model)
			results[i] = *result
		}
	}

	// Output results
	report.ConsoleReport(results, resolved.SDKVersion, resolved.CogVersion)

	// Check for failures
	var failedNames []string
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			failedNames = append(failedNames, r.Name)
		}
	}
	if len(failedNames) > 0 {
		return formatFailureSummary("build", results)
	}

	return nil
}
