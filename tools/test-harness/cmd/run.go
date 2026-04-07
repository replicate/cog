package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
	"github.com/replicate/cog/tools/test-harness/internal/report"
	"github.com/replicate/cog/tools/test-harness/internal/resolver"
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

	// Load manifest
	mf, manifestPath, err := manifest.Load(manifestPath)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}
	fmt.Printf("Loaded manifest: %s\n", manifestPath)

	// Resolve versions
	fmt.Println("Resolving versions...")
	resolved, err := resolver.Resolve(cogBinary, cogVersion, cogRef, sdkVersion, sdkWheel, map[string]string{
		"sdk_version": mf.Defaults.SDKVersion,
		"cog_version": mf.Defaults.CogVersion,
	})
	if err != nil {
		return fmt.Errorf("resolving versions: %w", err)
	}
	fmt.Printf("Using cog CLI: %s (%s)\n", resolved.CogBinary, resolved.CogVersion)
	if resolved.SDKWheel != "" {
		fmt.Printf("Using SDK wheel: %s\n", resolved.SDKWheel)
	} else {
		fmt.Printf("Using SDK version: %s\n", resolved.SDKVersion)
	}

	// Filter models
	models := mf.FilterModels(modelFilter, noGPU, gpuOnly)
	if len(models) == 0 {
		fmt.Println("No models to run")
		return nil
	}
	fmt.Printf("Running %d model(s)\n\n", len(models))

	// Create runner
	r, err := runner.New(resolved.CogBinary, resolved.SDKPatchVersion, resolved.SDKWheel, "", keepImages)
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}
	defer r.Cleanup()

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
			defer f.Close()
			if err := report.WriteJSONReport(results, resolved.SDKVersion, resolved.CogVersion, f); err != nil {
				return fmt.Errorf("writing JSON report: %w", err)
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
			defer f.Close()
			if err := report.WriteJSONReport(results, resolved.SDKVersion, resolved.CogVersion, f); err != nil {
				return fmt.Errorf("writing JSON report: %w", err)
			}
		}
	}

	// Check for failures
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			return fmt.Errorf("some tests failed")
		}
	}

	return nil
}
