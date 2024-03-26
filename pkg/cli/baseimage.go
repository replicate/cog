package cli

import (
	"fmt"
	"github.com/replicate/cog/pkg/config"
	"os"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
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
		Short:   "Cog base image commands",
		Version: fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
		// This stops errors being printed because we print them in cmd/cog/cog.go
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if global.Debug {
				console.SetLevel(console.DebugLevel)
			}
			cmd.SilenceUsage = true
			if err := update.DisplayAndCheckForRelease(); err != nil {
				console.Debugf("%s", err)
			}
		},
		SilenceErrors: true,
	}
	setPersistentFlags(&rootCmd)

	rootCmd.AddCommand(
		newBaseImageDockerfileCommand(),
		newBaseImageBuildCommand(),
	)

	return &rootCmd, nil
}

func newBaseImageDockerfileCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "dockerfile",
		Short: "Display Cog base image Dockerfile",
		RunE: func(cmd *cobra.Command, args []string) error {
			generator, err := baseImageGeneratorFromFlags()
			if err != nil {
				return err
			}
			dockerfile, err := generator.GenerateDockerfile()
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
			generator, err := baseImageGeneratorFromFlags()
			if err != nil {
				return err
			}
			dockerfileContents, err := generator.GenerateDockerfile()
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			baseImageName := dockerfile.BaseImageName(baseImageCUDAVersion, baseImagePythonVersion, baseImageTorchVersion)

			err = docker.Build(cwd, dockerfileContents, baseImageName, []string{}, buildNoCache, buildProgressOutput, config.BuildSourceEpochTimestamp)
			if err != nil {
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
	cmd.Flags().Int64VarP(&config.BuildSourceEpochTimestamp, "timestamp", "t", -1, "Set layer timestamps to this value (useful for reproducibility)")
	_ = cmd.Flags().MarkHidden("timestamp")
}

func baseImageGeneratorFromFlags() (*dockerfile.BaseImageGenerator, error) {
	return dockerfile.NewBaseImageGenerator(
		baseImageCUDAVersion,
		baseImagePythonVersion,
		baseImageTorchVersion,
	)

}
