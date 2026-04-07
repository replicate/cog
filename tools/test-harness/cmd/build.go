package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
	"github.com/replicate/cog/tools/test-harness/internal/report"
	"github.com/replicate/cog/tools/test-harness/internal/resolver"
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
		fmt.Println("No models to build")
		return nil
	}
	fmt.Printf("Building %d model(s)\n\n", len(models))

	// Create runner
	r, err := runner.New(resolved.CogBinary, resolved.SDKPatchVersion, resolved.SDKWheel, "", keepImages)
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
	for _, r := range results {
		if !r.Passed && !r.Skipped {
			return fmt.Errorf("some builds failed")
		}
	}

	return nil
}
