package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/cogpack"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
)

func newPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Generate and display the build plan for the current project",
		Long: `Generate and display the build plan showing what stages and operations would be executed during build.

This command shows the cogpack build plan in JSON format, including:
- Plan result metadata (stack, blocks, base image info)
- Final normalized Plan struct (stages, operations, contexts)

Note: This command requires the COGPACK experimental feature to be enabled.
Set COGPACK=1 in your environment to use this feature.`,
		RunE: cmdPlan,
		Args: cobra.NoArgs,
	}

	addConfigFlag(cmd)
	return cmd
}

func cmdPlan(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Check if cogpack is enabled
	if !cogpack.Enabled() {
		return fmt.Errorf("cogpack is not enabled. Set COGPACK=1 environment variable to use this feature")
	}

	// Load configuration
	cfg, projectDir, err := config.GetConfig(configFilename)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create source info
	src, err := cogpack.NewSourceInfo(projectDir, cfg)
	if err != nil {
		return fmt.Errorf("failed to create source info: %w", err)
	}
	defer src.Close()

	// Generate plan
	console.Infof("Generating build plan for project in %s...", projectDir)
	planResult, err := cogpack.GeneratePlan(ctx, src)
	if err != nil {
		return fmt.Errorf("failed to generate plan: %w", err)
	}

	// Format as pretty JSON
	util.JSONPrettyPrint(planResult)

	return nil
}
