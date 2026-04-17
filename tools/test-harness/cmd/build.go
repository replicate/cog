package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
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
	if err := validateConcurrency(); err != nil {
		return err
	}

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
		Parallel:    parallel,
	})
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}
	defer func() { _ = r.Cleanup() }()

	// Build models
	results := make([]report.ModelResult, len(models))

	runModels(ctx, models, results, parallel,
		func(ctx context.Context, model manifest.Model) *report.ModelResult {
			return r.BuildModel(ctx, model)
		},
		func(index, total int, model manifest.Model) string {
			if parallel {
				return fmt.Sprintf("  [%d/%d] Building %s...\n", index, total, model.Name)
			}
			return fmt.Sprintf("Building %s...\n", model.Name)
		},
		func(index, total int, model manifest.Model, result *report.ModelResult) string {
			if parallel {
				switch {
				case result.Passed:
					return fmt.Sprintf("  [%d/%d] + %s (%.1fs)\n", index, total, model.Name, result.BuildDuration)
				case result.Skipped:
					return fmt.Sprintf("  [%d/%d] - %s (skipped: %s)\n", index, total, model.Name, result.SkipReason)
				default:
					return fmt.Sprintf("  [%d/%d] x %s FAILED\n", index, total, model.Name)
				}
			}
			switch {
			case result.Passed:
				return fmt.Sprintf("  + %s built successfully (%.1fs)\n", model.Name, result.BuildDuration)
			case result.Skipped:
				return fmt.Sprintf("  - %s (skipped: %s)\n", model.Name, result.SkipReason)
			default:
				return fmt.Sprintf("  x %s FAILED\n", model.Name)
			}
		},
	)

	// Output results
	report.ConsoleReport(results, resolved.SDKVersion, resolved.CogVersion)

	// Check for failures
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			return formatFailureSummary("build", results)
		}
	}

	return nil
}
