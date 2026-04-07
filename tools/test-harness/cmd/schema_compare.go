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

func newSchemaCompareCommand() *cobra.Command {
	var outputFormat string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "schema-compare",
		Short: "Compare static vs runtime schema generation",
		Long:  `Build each model twice (with and without COG_STATIC_SCHEMA=1) and compare the generated OpenAPI schemas.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchemaCompare(cmd.Context(), outputFormat, outputFile)
		},
	}

	cmd.Flags().StringVar(&outputFormat, "output", "console", "Output format (console or json)")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write report to file")

	return cmd
}

func runSchemaCompare(ctx context.Context, outputFormat, outputFile string) error {

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

	// Filter models
	models := mf.FilterModels(modelFilter, noGPU, gpuOnly)
	if len(models) == 0 {
		fmt.Println("No models to compare")
		return nil
	}
	fmt.Printf("Comparing schemas for %d model(s)\n\n", len(models))

	// Create runner
	r, err := runner.New(resolved.CogBinary, resolved.SDKPatchVersion, resolved.SDKWheel, "", keepImages)
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}
	defer r.Cleanup()

	// Compare schemas
	var results []report.SchemaCompareResult
	for _, model := range models {
		fmt.Printf("Comparing %s...\n", model.Name)
		result := r.CompareSchema(ctx, model)
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
			if err := report.WriteSchemaCompareJSONReport(results, resolved.CogVersion, f); err != nil {
				return fmt.Errorf("writing schema compare JSON report: %w", err)
			}
		} else {
			if err := report.WriteSchemaCompareJSONReport(results, resolved.CogVersion, os.Stdout); err != nil {
				return fmt.Errorf("writing schema compare JSON report: %w", err)
			}
		}
	} else {
		report.SchemaCompareConsoleReport(results, resolved.CogVersion)
		if outputFile != "" {
			f, err := os.Create(outputFile)
			if err != nil {
				return fmt.Errorf("creating output file: %w", err)
			}
			defer f.Close()
			if err := report.WriteSchemaCompareJSONReport(results, resolved.CogVersion, f); err != nil {
				return fmt.Errorf("writing schema compare JSON report: %w", err)
			}
		}
	}

	// Check for failures
	for _, r := range results {
		if !r.Passed {
			return fmt.Errorf("some schema comparisons failed")
		}
	}

	return nil
}
