package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/registry"
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
var buildStrip bool
var buildPrecompile bool
var buildFast bool
var buildLocalImage bool
var configFilename string

const useCogBaseImageFlagKey = "use-cog-base-image"

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
	addStripFlag(cmd)
	addPrecompileFlag(cmd)
	addFastFlag(cmd)
	addLocalImage(cmd)
	addConfigFlag(cmd)
	addPipelineImage(cmd)
	cmd.Flags().StringVarP(&buildTag, "tag", "t", "", "A name for the built image in the form 'repository:tag'")
	return cmd
}

func buildCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	client, err := http.ProvideHTTPClient(ctx, dockerClient)
	if err != nil {
		return err
	}
	logClient := coglog.NewClient(client)
	logCtx := logClient.StartBuild(buildLocalImage)

	cfg, projectDir, err := config.GetConfig(configFilename)
	if err != nil {
		logClient.EndBuild(ctx, err, logCtx)
		return err
	}
	// In case one of `--x-fast` & `fast: bool` is set
	if cfg.Build.Fast {
		buildFast = cfg.Build.Fast
	}
	logCtx.Fast = buildFast
	logCtx.CogRuntime = false
	if cfg.Build.CogRuntime != nil {
		logCtx.CogRuntime = *cfg.Build.CogRuntime
	}

	imageName := cfg.Image
	if buildTag != "" {
		imageName = buildTag
	}
	if imageName == "" {
		imageName = config.DockerImageName(projectDir)
	}

	err = config.ValidateModelPythonVersion(cfg)
	if err != nil {
		logClient.EndBuild(ctx, err, logCtx)
		return err
	}
	registryClient := registry.NewRegistryClient()
	if err := image.Build(
		ctx,
		cfg,
		projectDir,
		imageName,
		buildSecrets,
		buildNoCache,
		buildSeparateWeights,
		buildUseCudaBaseImage,
		buildProgressOutput,
		buildSchemaFile,
		buildDockerfileFile,
		DetermineUseCogBaseImage(cmd),
		buildStrip,
		buildPrecompile,
		buildFast,
		nil,
		buildLocalImage,
		dockerClient,
		registryClient,
		pipelinesImage); err != nil {
		logClient.EndBuild(ctx, err, logCtx)
		return err
	}

	console.Infof("\nImage built as %s", imageName)
	logClient.EndBuild(ctx, nil, logCtx)

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
	cmd.Flags().BoolVar(&buildUseCogBaseImage, useCogBaseImageFlagKey, true, "Use pre-built Cog base image for faster cold boots")
}

func addBuildTimestampFlag(cmd *cobra.Command) {
	cmd.Flags().Int64Var(&config.BuildSourceEpochTimestamp, "timestamp", -1, "Number of seconds sing Epoch to use for the build timestamp; this rewrites the timestamp of each layer. Useful for reproducibility. (`-1` to disable timestamp rewrites)")
	_ = cmd.Flags().MarkHidden("timestamp")
}

func addStripFlag(cmd *cobra.Command) {
	const stripFlag = "strip"
	cmd.Flags().BoolVar(&buildStrip, stripFlag, false, "Whether to strip shared libraries for faster inference times")
	_ = cmd.Flags().MarkHidden(stripFlag)
}

func addPrecompileFlag(cmd *cobra.Command) {
	const precompileFlag = "precompile"
	cmd.Flags().BoolVar(&buildPrecompile, precompileFlag, false, "Whether to precompile python files for faster load times")
	_ = cmd.Flags().MarkHidden(precompileFlag)
}

func addFastFlag(cmd *cobra.Command) {
	const fastFlag = "x-fast"
	cmd.Flags().BoolVar(&buildFast, fastFlag, false, "Whether to use the experimental fast features")
	_ = cmd.Flags().MarkHidden(fastFlag)
}

func addLocalImage(cmd *cobra.Command) {
	const localImage = "x-localimage"
	cmd.Flags().BoolVar(&buildLocalImage, localImage, false, "Whether to use the experimental local image features")
	_ = cmd.Flags().MarkHidden(localImage)
}

func addConfigFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&configFilename, "file", "f", "cog.yaml", "The name of the config file.")
}

func checkMutuallyExclusiveFlags(cmd *cobra.Command, args []string) error {
	flags := []string{useCogBaseImageFlagKey, "use-cuda-base-image", "dockerfile"}
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

func DetermineUseCogBaseImage(cmd *cobra.Command) *bool {
	if !cmd.Flags().Changed(useCogBaseImageFlagKey) {
		return nil
	}
	useCogBaseImage := new(bool)
	*useCogBaseImage = buildUseCogBaseImage
	return useCogBaseImage
}
