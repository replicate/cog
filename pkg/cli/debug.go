package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sieve-data/cog/pkg/config"
	"github.com/sieve-data/cog/pkg/dockerfile"
	"github.com/sieve-data/cog/pkg/global"
	"github.com/sieve-data/cog/pkg/util/console"
)

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "debug",
		Hidden: true,
		RunE:   cmdDockerfile,
	}

	debug := &cobra.Command{
		Use:    "debug",
		Short:  "Generate a Dockerfile from " + global.ConfigFilename,
		Hidden: true,
	}

	cmd.AddCommand(debug)

	return cmd
}

func cmdDockerfile(cmd *cobra.Command, args []string) error {
	config, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	generator, err := dockerfile.NewGenerator(config, projectDir)
	if err != nil {
		return fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up after build: %v", err)
		}
	}()
	out, err := generator.Generate()
	if err != nil {
		return err
	}
	console.Output(out)
	return nil
}
