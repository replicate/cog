package cli

import (
	"context"
	"fmt"
	"path/filepath"
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

	pushOpts := provider.PushOptions{
		Image:      imageName,
		Config:     src.Config,
		ProjectDir: src.ProjectDir,
	}

	// Build the image
	buildID, _ := uuid.NewV7()
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

	// Push the model
	console.Infof("\nPushing image '%s'...", m.ImageRef())

	var pushErr error
	if m.ImageFormat == model.FormatBundle {
		// Bundle format: use resolver.Push which builds OCI index with weights
		filePaths, err := resolveWeightFilePaths(src)
		if err != nil {
			_ = p.PostPush(ctx, pushOpts, err)
			return fmt.Errorf("failed to resolve weight file paths: %w", err)
		}

		pushErr = resolver.Push(ctx, m, model.PushOptions{
			ProjectDir: src.ProjectDir,
			FilePaths:  filePaths,
		})
	} else {
		// Standalone format: use standard docker push
		pushErr = docker.Push(ctx, m.ImageRef(), src.ProjectDir, dockerClient, docker.BuildInfo{
			BuildTime: buildDuration,
			BuildID:   buildID.String(),
		}, httpClient)
	}

	// PostPush: the provider handles formatting errors and showing success messages
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

// resolveWeightFilePaths generates a map of weight names to their absolute file paths.
// This re-runs the weights lock generator to get the file paths, since they're not
// stored in the lock file itself.
func resolveWeightFilePaths(src *model.Source) (map[string]string, error) {
	if src.Config == nil || len(src.Config.Weights) == 0 {
		return nil, fmt.Errorf("no weights configured in cog.yaml")
	}

	gen := model.NewWeightsLockGenerator(model.WeightsLockGeneratorOptions{
		DestPrefix: "/cache",
	})

	// Use context.Background() since this is a short-lived operation
	_, filePaths, err := gen.GenerateWithFilePaths(context.Background(), src.ProjectDir, src.Config.Weights)
	if err != nil {
		return nil, err
	}

	// Convert to absolute paths
	absFilePaths := make(map[string]string, len(filePaths))
	for name, path := range filePaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for %s: %w", name, err)
		}
		absFilePaths[name] = absPath
	}

	return absFilePaths, nil
}
