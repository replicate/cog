package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

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
	addConfigFlag(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Initialize the provider registry
	setup.Init()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	httpClient, err := http.ProvideHTTPClient(ctx, dockerClient)
	if err != nil {
		return err
	}

	src, err := model.NewSource(configFilename)
	if err != nil {
		return err
	}

	imageName := src.Config.Image
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

	// Build push options
	buildID, err := uuid.NewV7()
	if err != nil {
		// Don't insert build ID but continue anyways
		console.Debugf("Failed to create build ID %v", err)
	}

	pushOpts := provider.PushOptions{
		Image:      imageName,
		Config:     src.Config,
		ProjectDir: src.ProjectDir,
		BuildID:    buildID.String(),
		HTTPClient: httpClient,
	}

	// PrePush: validation and setup (analytics start, feature checks)
	if err := p.PrePush(ctx, pushOpts); err != nil {
		return err
	}

	// Build the image
	annotations := map[string]string{}
	if buildID.String() != "" {
		annotations["run.cog.push_id"] = buildID.String()
	}

	startBuildTime := time.Now()
	regClient := registry.NewRegistryClient()
	resolver := model.NewResolver(dockerClient, regClient)

	// Build the model
	buildOpts := buildOptionsFromFlags(cmd, imageName, annotations)
	m, err := resolver.Build(ctx, src, buildOpts)
	if err != nil {
		// Call PostPush to handle error logging/analytics
		_ = p.PostPush(ctx, pushOpts, err)
		return err
	}

	buildDuration := time.Since(startBuildTime)

	// Log weights info for bundle format
	if m.ImageFormat == model.FormatBundle && m.WeightsManifest != nil {
		console.Infof("\nBundle format: %d weight files (%.2f MB)",
			len(m.WeightsManifest.Files), float64(m.WeightsManifest.TotalSize())/1024/1024)
	}

	// Push the image
	console.Infof("\nPushing image '%s'...", m.ImageRef())

	pushErr := docker.Push(ctx, m.ImageRef(), src.ProjectDir, dockerClient, docker.BuildInfo{
		BuildTime: buildDuration,
		BuildID:   buildID.String(),
	}, httpClient)

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
