package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/dockercontext"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/update"
	"github.com/replicate/cog/pkg/util/console"
)

var (
	baseImageCUDAVersion   string
	baseImagePythonVersion string
	baseImageTorchVersion  string
)

func NewBaseImageRootCommand() (*cobra.Command, error) {
	rootCmd := cobra.Command{
		Use:     "base-image",
		Short:   "Cog base image commands. This is an experimental feature with no guarantees of future support.",
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/cog/cog.go
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if global.Debug {
				console.SetLevel(console.DebugLevel)
			}
			cmd.SilenceUsage = true
			if err := update.DisplayAndCheckForRelease(cmd.Context()); err != nil {
				console.Debugf("%s", err)
			}
		},
		SilenceErrors: true,
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBaseImageDockerfileCommand(),
		newBaseImageBuildCommand(),
		newBaseImageGenerateMatrix(),
	)

	return &rootCmd, nil
}

func newBaseImageGenerateMatrix() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "generate-matrix",
		Short: "Generate a matrix of Cog base image versions (JSON)",
		RunE: func(cmd *cobra.Command, args []string) error {
			validCudaVersions := strings.FieldsFunc(baseImageCUDAVersion, func(c rune) bool {
				return c == ','
			})
			validPythonVersions := strings.FieldsFunc(baseImagePythonVersion, func(c rune) bool {
				return c == ','
			})
			validTorchVersions := strings.FieldsFunc(baseImageTorchVersion, func(c rune) bool {
				return c == ','
			})

			allConfigurations := dockerfile.BaseImageConfigurations()
			filteredMatrix := make([]dockerfile.BaseImageConfiguration, 0, len(allConfigurations))
			for _, config := range allConfigurations {
				var found bool
				if len(validCudaVersions) > 0 {
					found = false
					for _, validCudaVersion := range validCudaVersions {
						if config.CUDAVersion == validCudaVersion {
							found = true
						}
					}
					if !found {
						continue
					}
				}

				if len(validPythonVersions) > 0 {
					found = false
					for _, validPythonVersion := range validPythonVersions {
						if config.PythonVersion == validPythonVersion {
							found = true
						}
					}
					if !found {
						continue
					}
				}

				if len(validTorchVersions) > 0 {
					found = false
					for _, validTorchVersion := range validTorchVersions {
						if config.TorchVersion == validTorchVersion {
							found = true
						}
					}
					if !found {
						continue
					}
				}

				filteredMatrix = append(filteredMatrix, config)
			}

			output, err := json.Marshal(filteredMatrix)
			if err != nil {
				return err
			}
			fmt.Println(string(output))
			return nil
		},
		Args: cobra.MaximumNArgs(0),
	}
	addBaseImageFlags(cmd)
	return cmd
}

func newBaseImageDockerfileCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "dockerfile",
		Short: "Display Cog base image Dockerfile",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			generator, err := baseImageGeneratorFromFlags(ctx)
			if err != nil {
				return err
			}
			dockerfile, err := generator.GenerateDockerfile(ctx)
			if err != nil {
				return err
			}
			fmt.Println(dockerfile)
			return nil
		},
		Args: cobra.MaximumNArgs(0),
	}
	addBaseImageFlags(cmd)
	addNoCacheFlag(cmd)
	addBuildProgressOutputFlag(cmd)

	return cmd
}

func newBaseImageBuildCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "build",
		Short: "Build Cog base image",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			dockerClient, err := docker.NewClient(ctx)
			if err != nil {
				return err
			}

			generator, err := baseImageGeneratorFromFlags(ctx)
			if err != nil {
				return err
			}
			dockerfileContents, err := generator.GenerateDockerfile(ctx)
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			baseImageName := dockerfile.BaseImageName(baseImageCUDAVersion, baseImagePythonVersion, baseImageTorchVersion)

			buildOpts := command.ImageBuildOptions{
				WorkingDir:         cwd,
				DockerfileContents: dockerfileContents,
				ImageName:          baseImageName,
				NoCache:            buildNoCache,
				ProgressOutput:     buildProgressOutput,
				Epoch:              &config.BuildSourceEpochTimestamp,
				ContextDir:         dockercontext.StandardBuildDirectory,
			}
			if err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
				return err
			}
			fmt.Println("Successfully built image: " + baseImageName)
			return nil
		},
		Args: cobra.MaximumNArgs(0),
	}
	addBaseImageFlags(cmd)

	return cmd
}

func addBaseImageFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&baseImageCUDAVersion, "cuda", "", "CUDA version")
	cmd.Flags().StringVar(&baseImagePythonVersion, "python", "", "Python version")
	cmd.Flags().StringVar(&baseImageTorchVersion, "torch", "", "Torch version")
	addBuildTimestampFlag(cmd)
}

func baseImageGeneratorFromFlags(ctx context.Context) (*dockerfile.BaseImageGenerator, error) {
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	client := registry.NewRegistryClient()
	return dockerfile.NewBaseImageGenerator(
		ctx,
		client,
		baseImageCUDAVersion,
		baseImagePythonVersion,
		baseImageTorchVersion,
		dockerClient,
		true,
	)
}
