package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

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
	fmt.Printf("Running %d model(s)\n\n", len(models))

	// Create runner
	r, err := runner.New(runner.Options{
		CogBinary:   resolved.CogBinary,
		SDKVersion:  resolved.SDKPatchVersion,
		SDKWheel:    resolved.SDKWheel,
		CleanImages: cleanImages,
		KeepOutputs: keepOutputs,
	})
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}
	defer func() { _ = r.Cleanup() }()

	// Run tests
	var results []report.ModelResult
	for _, model := range models {
		fmt.Printf("Running %s...\n", model.Name)
		result := r.RunModel(ctx, model)
		results = append(results, *result)
	}

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
	var failedNames []string
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			failedNames = append(failedNames, r.Name)
		}
	}
	if len(failedNames) > 0 {
		return fmt.Errorf("%d model(s) failed: %s", len(failedNames), strings.Join(failedNames, ", "))
	}

	return nil
}
