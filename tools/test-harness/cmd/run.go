package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
	"github.com/replicate/cog/tools/test-harness/internal/report"
	"github.com/replicate/cog/tools/test-harness/internal/runner"
)

func newRunCommand() *cobra.Command {
	var outputFormat string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Build and test models",
		Long:  `Build Docker images and run tests for all models in the manifest.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd.Context(), outputFormat, outputFile)
		},
	}

	cmd.Flags().StringVar(&outputFormat, "output", "console", "Output format (console or json)")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write report to file")

	return cmd
}

func runRun(ctx context.Context, outputFormat, outputFile string) error {
	if outputFormat != "console" && outputFormat != "json" {
		return fmt.Errorf("invalid output format %q: must be 'console' or 'json'", outputFormat)
	}

	if err := validateConcurrency(); err != nil {
		return err
	}

	_, models, resolved, err := resolveSetup()
	if err != nil {
		return err
	}

	if resolved.SDKWheel != "" {
		fmt.Printf("Using SDK wheel: %s\n", resolved.SDKWheel)
	} else {
		fmt.Printf("Using SDK version: %s\n", resolved.SDKVersion)
	}

	if len(models) == 0 {
		fmt.Println("No models to run")
		return nil
	}

	parallel := concurrency > 1 && len(models) > 1
	if parallel {
		fmt.Printf("Running %d model(s) with concurrency %d\n\n", len(models), concurrency)
	} else {
		fmt.Printf("Running %d model(s)\n\n", len(models))
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

	// Run tests
	results := make([]report.ModelResult, len(models))

	runModels(ctx, models, results, parallel,
		func(ctx context.Context, model manifest.Model) *report.ModelResult {
			return r.RunModel(ctx, model)
		},
		func(index, total int, model manifest.Model) string {
			if parallel {
				return fmt.Sprintf("  [%d/%d] Running %s...\n", index, total, model.Name)
			}
			return fmt.Sprintf("Running %s...\n", model.Name)
		},
		func(index, total int, model manifest.Model, result *report.ModelResult) string {
			testCount := len(result.TestResults) + len(result.TrainResults)
			if parallel {
				switch {
				case result.Skipped:
					return fmt.Sprintf("  [%d/%d] - %s (skipped: %s)\n", index, total, model.Name, result.SkipReason)
				case result.Passed:
					return fmt.Sprintf("  [%d/%d] + %s (%.1fs build, %d tests passed)\n", index, total, model.Name, result.BuildDuration, testCount)
				default:
					return fmt.Sprintf("  [%d/%d] x %s FAILED\n", index, total, model.Name)
				}
			}
			switch {
			case result.Skipped:
				return fmt.Sprintf("  - %s (skipped: %s)\n", model.Name, result.SkipReason)
			case result.Passed:
				return fmt.Sprintf("  + %s (%.1fs build, %d tests passed)\n", model.Name, result.BuildDuration, testCount)
			default:
				return fmt.Sprintf("  x %s FAILED\n", model.Name)
			}
		},
	)

	// Output results
	if outputFormat == "json" {
		if outputFile != "" {
			f, err := os.Create(outputFile)
			if err != nil {
				return fmt.Errorf("creating output file: %w", err)
			}
			writeErr := report.WriteJSONReport(results, resolved.SDKVersion, resolved.CogVersion, f)
			if closeErr := f.Close(); closeErr != nil && writeErr == nil {
				writeErr = closeErr
			}
			if writeErr != nil {
				return fmt.Errorf("writing JSON report: %w", writeErr)
			}
		} else {
			if err := report.WriteJSONReport(results, resolved.SDKVersion, resolved.CogVersion, os.Stdout); err != nil {
				return fmt.Errorf("writing JSON report: %w", err)
			}
		}
	} else {
		report.ConsoleReport(results, resolved.SDKVersion, resolved.CogVersion)
		if outputFile != "" {
			f, err := os.Create(outputFile)
			if err != nil {
				return fmt.Errorf("creating output file: %w", err)
			}
			writeErr := report.WriteJSONReport(results, resolved.SDKVersion, resolved.CogVersion, f)
			if closeErr := f.Close(); closeErr != nil && writeErr == nil {
				writeErr = closeErr
			}
			if writeErr != nil {
				return fmt.Errorf("writing JSON report: %w", writeErr)
			}
		}
	}

	// Check for failures
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			return formatFailureSummary("model", results)
		}
	}

	return nil
}
