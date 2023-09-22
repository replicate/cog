package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/util/console"
)

var buildTag string
var buildSeparateWeights bool
var buildSecrets []string
var buildNoCache bool
var buildProgressOutput string
var buildSchemaFile string
var buildUseCudaBaseImage string
var buildDockerfileFile string

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build an image from cog.yaml",
		Args:  cobra.NoArgs,
		RunE:  buildCommand,
	}
	addBuildProgressOutputFlag(cmd)
	addSecretsFlag(cmd)
	addNoCacheFlag(cmd)
	addSeparateWeightsFlag(cmd)
	addSchemaFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
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

	if err := image.Build(cfg, projectDir, imageName, buildSecrets, buildNoCache, buildSeparateWeights, buildUseCudaBaseImage, buildProgressOutput, buildSchemaFile, buildDockerfileFile); err != nil {
		return err
	}

	console.Infof("\nImage built as %s", imageName)

	return nil
}

func addBuildProgressOutputFlag(cmd *cobra.Command) {
	defaultOutput := "auto"
	if os.Getenv("TERM") == "dumb" {
		defaultOutput = "plain"
	}
	cmd.Flags().StringVar(&buildProgressOutput, "progress", defaultOutput, "Set type of build progress output, 'auto' (default), 'tty' or 'plain'")
}

func addSecretsFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&buildSecrets, "secret", []string{}, "Secrets to pass to the build environment in the form 'id=foo,src=/path/to/file'")
}

func addNoCacheFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&buildNoCache, "no-cache", false, "Do not use cache when building the image")
}

func addSeparateWeightsFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&buildSeparateWeights, "separate-weights", false, "Separate model weights from code in image layers")
}

func addSchemaFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&buildSchemaFile, "openapi-schema", "", "Load OpenAPI schema from a file")
}

func addUseCudaBaseImageFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&buildUseCudaBaseImage, "use-cuda-base-image", "auto", "Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects")
}

func addDockerfileFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&buildDockerfileFile, "dockerfile", "", "Path to a Dockerfile. If the flag is passed but no value is provided, defaults to the Dockerfile in the working directory.")
}
