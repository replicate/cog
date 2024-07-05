package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
var buildUseCogBaseImage bool

func newBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "build",
		Short:   "Build an image from cog.yaml",
		Args:    cobra.NoArgs,
		RunE:    buildCommand,
		PreRunE: checkMutuallyExclusiveFlags,
	}
	addBuildProgressOutputFlag(cmd)
	addSecretsFlag(cmd)
	addNoCacheFlag(cmd)
	addSeparateWeightsFlag(cmd)
	addSchemaFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addBuildTimestampFlag(cmd)
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

	err = config.ValidateModelPythonVersion(cfg.Build.PythonVersion)
	if err != nil {
		return err
	}

	if err := image.Build(cfg, projectDir, imageName, buildSecrets, buildNoCache, buildSeparateWeights, buildUseCudaBaseImage, buildProgressOutput, buildSchemaFile, buildDockerfileFile, buildUseCogBaseImage); err != nil {
		if buildUseCogBaseImage && cmd.Flags().Changed("use-cog-base-image") {
			console.Infof("Build failed with Cog base image enabled by default. " +
				"If you want to build without using pre-built base images, " +
				"try `cog build --use-cog-base-image=false`.")
		}

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
	cmd.Flags().StringVar(&buildDockerfileFile, "dockerfile", "", "Path to a Dockerfile. If set, cog will use this Dockerfile instead of generating one from cog.yaml")
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "dockerfile" {
			f.Hidden = true
		}
	})
}

func addUseCogBaseImageFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&buildUseCogBaseImage, "use-cog-base-image", true, "Use pre-built Cog base image for faster cold boots")
}

func addBuildTimestampFlag(cmd *cobra.Command) {
	cmd.Flags().Int64Var(&config.BuildSourceEpochTimestamp, "timestamp", -1, "Number of seconds sing Epoch to use for the build timestamp; this rewrites the timestamp of each layer. Useful for reproducibility. (`-1` to disable timestamp rewrites)")
	_ = cmd.Flags().MarkHidden("timestamp")
}

func checkMutuallyExclusiveFlags(cmd *cobra.Command, args []string) error {
	flags := []string{"use-cog-base-image", "use-cuda-base-image", "dockerfile"}
	var flagsSet []string
	for _, flag := range flags {
		if cmd.Flag(flag).Changed {
			flagsSet = append(flagsSet, "--"+flag)
		}
	}
	if len(flagsSet) > 1 {
		return fmt.Errorf("The flags %s are mutually exclusive: you can only set one of them.", strings.Join(flagsSet, " and "))
	}
	return nil
}
