package kong

import (
	"fmt"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// DebugCmd implements `cog debug` (hidden).
type DebugCmd struct {
	SeparateWeights bool   `help:"Separate model weights from code" name:"separate-weights"`
	UseCudaBase     string `help:"Use Nvidia CUDA base image" name:"use-cuda-base-image" default:"auto"`
	Dockerfile      string `help:"Path to a Dockerfile" name:"dockerfile" hidden:""`
	UseCogBase      bool   `help:"Use pre-built Cog base image" name:"use-cog-base-image" default:"true" negatable:""`
	Timestamp       int64  `help:"Epoch seconds for reproducible builds" name:"timestamp" hidden:"" default:"-1"`
	ConfigFile      string `help:"Config file path" short:"f" name:"file" default:"cog.yaml"`
	ImageName       string `help:"Image name for generated Dockerfile" name:"image-name"`
}

func (c *DebugCmd) AfterApply() error {
	config.BuildSourceEpochTimestamp = c.Timestamp
	return nil
}

func (c *DebugCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	cfg, projectDir, err := config.GetConfig(c.ConfigFile)
	if err != nil {
		return err
	}

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	client := registry.NewRegistryClient()
	generator, err := dockerfile.NewGenerator(cfg, projectDir, dockerClient, client, true)
	if err != nil {
		return fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up after build: %v", err)
		}
	}()

	generator.SetUseCudaBaseImage(c.UseCudaBase)

	if c.SeparateWeights {
		imageName := c.ImageName
		if imageName == "" {
			imageName = config.DockerImageName(projectDir)
		}

		weightsDockerfile, runnerDockerfile, dockerignore, err := generator.GenerateModelBaseWithSeparateWeights(ctx, imageName)
		if err != nil {
			return err
		}

		console.Output(fmt.Sprintf("=== Weights Dockerfile contents:\n%s\n===\n", weightsDockerfile))
		console.Output(fmt.Sprintf("=== Runner Dockerfile contents:\n%s\n===\n", runnerDockerfile))
		console.Output(fmt.Sprintf("=== DockerIgnore contents:\n%s===\n", dockerignore))
	} else {
		df, err := generator.GenerateDockerfileWithoutSeparateWeights(ctx)
		if err != nil {
			return err
		}
		console.Output(df)
	}

	return nil
}
