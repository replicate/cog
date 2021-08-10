package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/util/console"
)

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "push [IMAGE]",

		Short:   "Build and push model in current directory to a Docker registry",
		Example: `cog push registry.hooli.corp/hotdog-detector`,
		RunE:    push,
		Args:    cobra.MaximumNArgs(1),
	}
	addBuildProgressOutputFlag(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}

	if imageName == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.hooli.corp/hotdog-detector'")
	}

	if err := image.Build(cfg, projectDir, imageName, buildProgressOutput); err != nil {
		return err
	}

	console.Infof("\nPushing image '%s'...", imageName)

	return docker.Push(imageName)
}
