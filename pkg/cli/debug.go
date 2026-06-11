package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
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
	addUseCogBaseImageFlag(cmd)
	addBuildTimestampFlag(cmd)
	addConfigFlag(cmd)
	cmd.Flags().StringVarP(&imageName, "image-name", "", "", "The image name to use for the generated Dockerfile")

	return cmd
}

func cmdDockerfile(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	return RunDebug(ctx, dockerClient, registry.NewRegistryClient(), DebugCommandOptions{
		ConfigFilename:   configFilename,
		ImageName:        imageName,
		SeparateWeights:  buildSeparateWeights,
		UseCudaBaseImage: buildUseCudaBaseImage,
		UseCogBaseImage:  DetermineUseCogBaseImage(cmd),
	})
}

// DebugCommandOptions holds the parser-independent options for the debug
// command, which generates and prints a Dockerfile.
type DebugCommandOptions struct {
	ConfigFilename   string
	ImageName        string
	SeparateWeights  bool
	UseCudaBaseImage string
	UseCogBaseImage  *bool
}

// RunDebug generates the Dockerfile(s) for the model and prints them to stdout.
// It is shared by both the Cobra and Kong debug commands.
func RunDebug(ctx context.Context, dockerClient command.Command, regClient registry.Client, opts DebugCommandOptions) error {
	src, err := model.NewSource(opts.ConfigFilename)
	if err != nil {
		return err
	}
	defer src.Close()

	buildDir, err := src.DotCog.TempPath("build")
	if err != nil {
		return err
	}

	generator, err := dockerfile.NewStandardGenerator(src.Config, src.ProjectDir, buildDir, src.ConfigFilename, dockerClient, regClient, true)
	if err != nil {
		return fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}

	generator.SetUseCudaBaseImage(opts.UseCudaBaseImage)
	if opts.UseCogBaseImage != nil {
		generator.SetUseCogBaseImage(*opts.UseCogBaseImage)
	}

	if opts.SeparateWeights {
		imageName := opts.ImageName
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
