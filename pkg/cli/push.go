package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

var pipelinesImage bool

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "push [IMAGE]",

		Short:   "Build and push model in current directory to a Docker registry",
		Example: `cog push registry.example.com/your-username/model-name`,
		RunE:    push,
		Args:    cobra.MaximumNArgs(1),
	}
	addSecretsFlag(cmd)
	addNoCacheFlag(cmd)
	addSeparateWeightsFlag(cmd)
	addSchemaFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
	addBuildProgressOutputFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addStripFlag(cmd)
	addPrecompileFlag(cmd)
	addFastFlag(cmd)
	addLocalImage(cmd)
	addConfigFlag(cmd)
	addPipelineImage(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Initialize the provider registry
	setup.Init()

	// Get config first to determine the image name
	cfg, projectDir, err := config.GetConfig(configFilename)
	if err != nil {
		return err
	}

	if buildFast {
		console.Warn("The `--x-fast` flag is deprecated and will be removed in future versions.")
	}

	// In case one of `--x-fast` & `fast: bool` is set
	if cfg.Build.Fast {
		buildFast = cfg.Build.Fast
	}

	// Determine image name
	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}

	if imageName == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.example.com/your-username/model-name'")
	}

	// Look up the provider for the target registry
	p := provider.DefaultRegistry().ForImage(imageName)
	if p == nil {
		return fmt.Errorf("no provider found for image '%s'", imageName)
	}

	// Set up clients
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	httpClient, err := http.ProvideHTTPClient(ctx, dockerClient)
	if err != nil {
		return err
	}

	// Build push options
	buildID, err := uuid.NewV7()
	if err != nil {
		// Don't insert build ID but continue anyways
		console.Debugf("Failed to create build ID %v", err)
	}

	pushOpts := provider.PushOptions{
		Image:      imageName,
		Config:     cfg,
		ProjectDir: projectDir,
		LocalImage: buildLocalImage,
		FastPush:   buildFast,
		BuildID:    buildID.String(),
		HTTPClient: httpClient,
	}

	// PrePush: validation and setup (analytics start, feature checks)
	if err := p.PrePush(ctx, pushOpts); err != nil {
		return err
	}

	// If PrePush warned about FastPush but didn't error, disable it
	// (GenericProvider warns but doesn't error for FastPush)
	if buildFast && p.Name() != "replicate" {
		buildFast = false
		pushOpts.FastPush = false
	}

	// Build the image
	annotations := map[string]string{}
	if buildID.String() != "" {
		annotations["run.cog.push_id"] = buildID.String()
	}

	startBuildTime := time.Now()
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
		annotations,
		buildLocalImage,
		dockerClient,
		registryClient,
		pipelinesImage); err != nil {
		return err
	}

	buildDuration := time.Since(startBuildTime)

	// Push the image
	console.Infof("\nPushing image '%s'...", imageName)

	pushErr := docker.Push(ctx, imageName, buildFast, projectDir, dockerClient, docker.BuildInfo{
		BuildTime: buildDuration,
		BuildID:   buildID.String(),
		Pipeline:  pipelinesImage,
	}, httpClient, cfg)

	// PostPush: cleanup, analytics end, success/error messages
	// The provider handles formatting errors and showing success messages
	if err := p.PostPush(ctx, pushOpts, pushErr); err != nil {
		return err
	}

	// If there was a push error but PostPush didn't return one,
	// return a generic error
	if pushErr != nil {
		return fmt.Errorf("failed to push image: %w", pushErr)
	}

	return nil
}

func addPipelineImage(cmd *cobra.Command) {
	const pipeline = "x-pipeline"
	cmd.Flags().BoolVar(&pipelinesImage, pipeline, false, "Whether to use the experimental pipeline feature")
	_ = cmd.Flags().MarkHidden(pipeline)
}
