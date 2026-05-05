package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

var imageName string

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "debug",
		Hidden: true,
		Short:  "Generate a Dockerfile from cog",
		RunE:   cmdDockerfile,
	}

	addSeparateWeightsFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addBuildTimestampFlag(cmd)
	addConfigFlag(cmd)
	cmd.Flags().StringVarP(&imageName, "image-name", "", "", "The image name to use for the generated Dockerfile")

	return cmd
}

func cmdDockerfile(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return err
	}
	defer src.Close()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	buildDir, err := src.DotCog.TempPath("build")
	if err != nil {
		return err
	}

	client := registry.NewRegistryClient()
	generator, err := dockerfile.NewStandardGenerator(src.Config, src.ProjectDir, buildDir, src.ConfigFilename, dockerClient, client, true)
	if err != nil {
		return fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}

	generator.SetUseCudaBaseImage(buildUseCudaBaseImage)
	useCogBaseImage := DetermineUseCogBaseImage(cmd)
	if useCogBaseImage != nil {
		generator.SetUseCogBaseImage(*useCogBaseImage)
	}

	if buildSeparateWeights {
		if imageName == "" {
			imageName = config.DockerImageName(src.ProjectDir)
		}

		weightsDockerfile, runnerDockerfile, weightsExclude, err := generator.GenerateModelBaseWithSeparateWeights(ctx, imageName)
		if err != nil {
			return err
		}

		console.Output(fmt.Sprintf("=== Weights Dockerfile contents:\n%s\n===\n", weightsDockerfile))
		console.Output(fmt.Sprintf("=== Runner Dockerfile contents:\n%s\n===\n", runnerDockerfile))
		console.Output(fmt.Sprintf("=== Weights exclude patterns:\n%s\n===\n", strings.Join(weightsExclude, "\n")))
	} else {
		dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(ctx)
		if err != nil {
			return err
		}

		console.Output(dockerfile)
	}

	return nil
}
