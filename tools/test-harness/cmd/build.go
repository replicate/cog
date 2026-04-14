package cmd

import (
	"context"
	"fmt"
	"strings"

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
	fmt.Printf("Building %d model(s)\n\n", len(models))

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

	// Build models
	var results []report.ModelResult
	for _, model := range models {
		fmt.Printf("Building %s...\n", model.Name)
		result := r.BuildModel(ctx, model)
		results = append(results, *result)
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
		return fmt.Errorf("%d build(s) failed: %s", len(failedNames), strings.Join(failedNames, ", "))
	}

	return nil
}
