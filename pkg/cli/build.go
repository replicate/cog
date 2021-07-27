package cli

import (
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/image"
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

	imageName := cfg.Image
	if buildTag != "" {
		imageName = buildTag
	}
	if imageName == "" {
		imageName = config.DockerImageName(projectDir)
	}

	if err := image.Build(cfg, projectDir, imageName); err != nil {
		return err
	}

	console.Infof("\nImage built as %s", imageName)

	return nil
}
