package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

var imageName string

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "debug",
		Hidden: true,
		Short:  "Generate a Dockerfile from " + global.ConfigFilename,
		RunE:   cmdDockerfile,
	}

	addSeparateWeightsFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addBuildTimestampFlag(cmd)
	cmd.Flags().StringVarP(&imageName, "image-name", "", "", "The image name to use for the generated Dockerfile")

	return cmd
}

func cmdDockerfile(cmd *cobra.Command, args []string) error {
	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	generator, err := dockerfile.NewGenerator(cfg, projectDir)
	if err != nil {
		return fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up after build: %v", err)
		}
	}()

	generator.SetUseCudaBaseImage(buildUseCudaBaseImage)
	generator.SetUseCogBaseImage(buildUseCogBaseImage)

	if buildSeparateWeights {
		if imageName == "" {
			imageName = config.DockerImageName(projectDir)
		}

		weightsDockerfile, RunnerDockerfile, dockerignore, err := generator.GenerateModelBaseWithSeparateWeights(imageName)
		if err != nil {
			return err
		}

		console.Output(fmt.Sprintf("=== Weights Dockerfile contents:\n%s\n===\n", weightsDockerfile))
		console.Output(fmt.Sprintf("=== Runner Dockerfile contents:\n%s\n===\n", RunnerDockerfile))
		console.Output(fmt.Sprintf("=== DockerIgnore contents:\n%s===\n", dockerignore))
	} else {
		dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
		if err != nil {
			return err
		}

		console.Output(dockerfile)
	}

	return nil
}
