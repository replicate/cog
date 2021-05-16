package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/serving"
)

func newTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Build and test a model's examples on local machine",
		RunE:  Test,
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringP("arch", "a", "cpu", "Test architecture")

	return cmd
}

func Test(cmd *cobra.Command, args []string) error {
	arch, err := cmd.Flags().GetString("arch")
	if err != nil {
		return err
	}

	config, projectDir, err := getConfig()
	if err != nil {
		return err
	}

	archMap := map[string]bool{}
	for _, confArch := range config.Environment.Architectures {
		archMap[confArch] = true
	}
	if _, ok := archMap[arch]; !ok {
		return fmt.Errorf("Architecture %s is not defined for model", arch)
	}
	generator := docker.NewDockerfileGenerator(config, arch, projectDir)
	dockerfileContents, err := generator.Generate()
	if err != nil {
		return fmt.Errorf("Failed to generate Dockerfile for %s: %w", arch, err)
	}
	defer generator.Cleanup()
	dockerImageBuilder := docker.NewLocalImageBuilder("")
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	logWriter := logger.NewConsoleLogger()
	buildUseGPU := config.Environment.BuildRequiresGPU && arch == "gpu"
	tag, err := dockerImageBuilder.Build(context.Background(), projectDir, dockerfileContents, "", buildUseGPU, logWriter)
	if err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}

	if _, err := serving.TestVersion(context.Background(), servingPlatform, tag, config.Examples, projectDir, arch == "gpu", logWriter); err != nil {
		return err
	}

	return nil
}
