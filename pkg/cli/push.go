package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/docker"
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

	// Log weights info
	weights := m.WeightArtifacts()
	if len(weights) > 0 {
		console.Infof("\n%d weight artifact(s)", len(weights))
	}

	// Push the model (image + optional weights)
	console.Infof("\nPushing image '%s'...", m.ImageRef())

	// Set up progress display using Docker's jsonmessage rendering. This uses the
	// same cursor movement and progress display as `docker push`, which handles
	// terminal resizing correctly (each line is erased and rewritten individually,
	// rather than relying on a bulk cursor-up count that can desync on resize).
	pw := newProgressWriter()
	defer pw.Close()

	pushErr := resolver.Push(ctx, m, model.PushOptions{
		ImageProgressFn: func(prog model.PushProgress) {
			// Truncate digest for display: "sha256:abc123..." → "abc123..."
			displayDigest := prog.LayerDigest
			if len(displayDigest) > 7+12 { // "sha256:" + 12 hex chars
				displayDigest = displayDigest[7:19] + "..."
			}

			pw.Write(displayDigest, "Pushing", prog.Complete, prog.Total)
		},
	})

	pw.Close()

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
