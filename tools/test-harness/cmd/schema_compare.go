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
	if outputFormat != "console" && outputFormat != "json" {
		return fmt.Errorf("invalid output format %q: must be 'console' or 'json'", outputFormat)
	}

	_, models, resolved, err := resolveSetup()
	if err != nil {
		return err
	}

	if len(models) == 0 {
		fmt.Println("No models to compare")
		return nil
	}
	fmt.Printf("Comparing schemas for %d model(s)\n\n", len(models))

	// Create runner
	r, err := runner.New(runner.Options{
		CogBinary:   resolved.CogBinary,
		SDKVersion:  resolved.SDKPatchVersion,
		SDKWheel:    resolved.SDKWheel,
		KeepImages:  keepImages,
		KeepOutputs: keepOutputs,
	})
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
			writeErr := report.WriteSchemaCompareJSONReport(results, resolved.CogVersion, f)
			if closeErr := f.Close(); closeErr != nil && writeErr == nil {
				writeErr = closeErr
			}
			if writeErr != nil {
				return fmt.Errorf("writing schema compare JSON report: %w", writeErr)
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
			writeErr := report.WriteSchemaCompareJSONReport(results, resolved.CogVersion, f)
			if closeErr := f.Close(); closeErr != nil && writeErr == nil {
				writeErr = closeErr
			}
			if writeErr != nil {
				return fmt.Errorf("writing schema compare JSON report: %w", writeErr)
			}
		}
	}

	// Check for failures
	var failedNames []string
	for _, r := range results {
		if !r.Passed {
			failedNames = append(failedNames, r.Name)
		}
	}
	if len(failedNames) > 0 {
		return fmt.Errorf("%d schema comparison(s) failed: %s", len(failedNames), strings.Join(failedNames, ", "))
	}

	return nil
}
