package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"

	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/update"
	"github.com/replicate/cog/pkg/util/console"
)

var (
	baseImageCUDAVersion         string
	baseImagePythonVersion       string
	baseImageTorchVersion        string
	baseImageBreakSystemPackages bool
	baseImageBuildContextDir     string
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
			return RunBaseImageGenerateMatrix(baseImageOptionsFromFlags())
		},
		Args: cobra.MaximumNArgs(0),
	}
	addBaseImageFlags(cmd)
	return cmd
}

// RunBaseImageGenerateMatrix prints, as JSON, the matrix of supported base image
// configurations filtered by the comma-separated CUDA/Python/Torch versions in
// opts. Empty version filters match all values. It is shared by both the Cobra
// and Kong base-image generate-matrix commands.
func RunBaseImageGenerateMatrix(opts BaseImageOptions) error {
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(c rune) bool { return c == ',' })
	}
	validCudaVersions := split(opts.CUDAVersion)
	validPythonVersions := split(opts.PythonVersion)
	validTorchVersions := split(opts.TorchVersion)

	matches := func(filters []string, value string) bool {
		return len(filters) == 0 || slices.Contains(filters, value)
	}

	allConfigurations := dockerfile.BaseImageConfigurations()
	filteredMatrix := make([]dockerfile.BaseImageConfiguration, 0, len(allConfigurations))
	for _, config := range allConfigurations {
		if !matches(validCudaVersions, config.CUDAVersion) {
			continue
		}
		if !matches(validPythonVersions, config.PythonVersion) {
			continue
		}
		if !matches(validTorchVersions, config.TorchVersion) {
			continue
		}
		filteredMatrix = append(filteredMatrix, config)
	}

	output, err := json.Marshal(filteredMatrix)
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	return nil
}

func newBaseImageDockerfileCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "dockerfile",
		Short: "Display Cog base image Dockerfile",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunBaseImageDockerfile(cmd.Context(), baseImageOptionsFromFlags())
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
			dockerClient, err := docker.NewClient(cmd.Context())
			if err != nil {
				return err
			}
			return RunBaseImageBuild(cmd.Context(), dockerClient, baseImageOptionsFromFlags())
		},
		Args: cobra.MaximumNArgs(0),
	}
	addBaseImageFlags(cmd)

	return cmd
}

// BaseImageOptions holds the parser-independent options shared by the
// base-image dockerfile and build commands.
type BaseImageOptions struct {
	CUDAVersion         string
	PythonVersion       string
	TorchVersion        string
	BreakSystemPackages bool
	BuildContextDir     string
	NoCache             bool
	ProgressOutput      string
	Timestamp           int64
}

// baseImageOptionsFromFlags reads the package-level Cobra flag globals into a
// BaseImageOptions value.
func baseImageOptionsFromFlags() BaseImageOptions {
	return BaseImageOptions{
		CUDAVersion:         baseImageCUDAVersion,
		PythonVersion:       baseImagePythonVersion,
		TorchVersion:        baseImageTorchVersion,
		BreakSystemPackages: baseImageBreakSystemPackages,
		BuildContextDir:     baseImageBuildContextDir,
		NoCache:             buildNoCache,
		ProgressOutput:      buildProgressOutput,
		Timestamp:           config.BuildSourceEpochTimestamp,
	}
}

// RunBaseImageDockerfile generates and prints the base image Dockerfile. It is
// shared by both the Cobra and Kong base-image dockerfile commands.
func RunBaseImageDockerfile(ctx context.Context, opts BaseImageOptions) error {
	generator, err := baseImageGenerator(ctx, opts)
	if err != nil {
		return err
	}
	contents, err := generator.GenerateDockerfile(ctx)
	if err != nil {
		return err
	}
	fmt.Println(contents)
	return nil
}

// RunBaseImageBuild builds a Cog base image. It is shared by both the Cobra and
// Kong base-image build commands.
func RunBaseImageBuild(ctx context.Context, dockerClient command.Command, opts BaseImageOptions) error {
	generator, err := baseImageGenerator(ctx, opts)
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
	baseImageName := dockerfile.BaseImageName(opts.CUDAVersion, opts.PythonVersion, opts.TorchVersion)

	timestamp := opts.Timestamp
	buildOpts := command.ImageBuildOptions{
		WorkingDir:         cwd,
		DockerfileContents: dockerfileContents,
		ImageName:          baseImageName,
		NoCache:            opts.NoCache,
		ProgressOutput:     opts.ProgressOutput,
		Epoch:              &timestamp,
		ContextDir:         ".",
	}
	if _, err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
		return err
	}
	fmt.Println("Successfully built image: " + baseImageName)
	return nil
}

func addBaseImageFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&baseImageCUDAVersion, "cuda", "", "CUDA version")
	cmd.Flags().StringVar(&baseImagePythonVersion, "python", "", "Python version")
	cmd.Flags().StringVar(&baseImageTorchVersion, "torch", "", "Torch version")
	cmd.Flags().BoolVar(&baseImageBreakSystemPackages, "break-system-packages", false, "Allow pip to modify uv-managed Python installs")
	_ = cmd.Flags().MarkHidden("break-system-packages")
	cmd.Flags().StringVar(&baseImageBuildContextDir, "build-context-dir", "", "Directory for generated Docker build context artifacts")
	_ = cmd.Flags().MarkHidden("build-context-dir")
	addBuildTimestampFlag(cmd)
}

func baseImageGenerator(ctx context.Context, opts BaseImageOptions) (*dockerfile.BaseImageGenerator, error) {
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	client := registry.NewRegistryClient()
	generator, err := dockerfile.NewBaseImageGenerator(
		ctx,
		client,
		opts.CUDAVersion,
		opts.PythonVersion,
		opts.TorchVersion,
		dockerClient,
		true,
	)
	if err != nil {
		return nil, err
	}
	generator.SetBreakSystemPackages(opts.BreakSystemPackages)
	generator.SetBuildContextDir(opts.BuildContextDir)
	return generator, nil
}
