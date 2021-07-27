package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/util/console"
)

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "push [IMAGE[:TAG]]",

		Short:   "Build and push model in current directory to a Docker registry",
		Example: `cog push registry.hooli.corp/hotdog-detector`,
		RunE:    push,
		Args:    cobra.MaximumNArgs(1),
	}

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	image := cfg.Image
	if len(args) > 0 {
		image = args[0]
	}

	if image == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.hooli.corp/hotdog-detector'")
	}

	console.Infof("Building Docker image from environment in cog.yaml as %s...\n\n", image)

	generator := dockerfile.NewGenerator(cfg, projectDir)
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	dockerfileContents, err := generator.Generate()
	if err != nil {
		return fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}

	if err := docker.Build(projectDir, dockerfileContents, image); err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}

	console.Infof("\nPushing image '%s'...", image)

	return docker.Push(image)
}
