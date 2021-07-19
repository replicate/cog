package cli

import (
	"fmt"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/spf13/cobra"
)

var buildTag string

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build an image from cog.yaml",
		Args:  cobra.NoArgs,
		RunE:  buildCommand,
	}
	cmd.Flags().StringVarP(&buildTag, "tag", "t", "", "A name for the built image in the form 'repository:tag'")
	return cmd
}

func buildCommand(cmd *cobra.Command, args []string) error {

	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	image := cfg.Image

	if buildTag != "" {
		image = buildTag
	}

	if image == "" {
		image = config.DockerImageName(projectDir)
	}

	console.Infof("Building Docker image from environment in cog.yaml as %s...", image)

	arch := "cpu"
	generator := dockerfile.NewGenerator(cfg, arch, projectDir)
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	dockerfileContents, err := generator.Generate()
	if err != nil {
		return fmt.Errorf("Failed to generate Dockerfile for %s: %w", arch, err)
	}

	if err := docker.Build(projectDir, dockerfileContents, image); err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}

	console.Infof("\nImage built as %s", image)

	return nil
}
