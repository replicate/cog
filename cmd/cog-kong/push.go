package main

import (
	"context"
	"fmt"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// PushCmd implements the "cog push" command.
type PushCmd struct {
	BuildFlags `embed:""`

	Image string `arg:"" optional:"" help:"Image name to push (e.g. registry.example.com/user/model)."`
}

// Validate is called by Kong after parsing, before Run.
func (cmd *PushCmd) Validate() error {
	return cmd.ValidateMutualExclusivity()
}

// Run executes the push command: build then push.
func (cmd *PushCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client, providerReg *provider.Registry, src *model.Source) error {
	imageName := src.Config.Image
	if cmd.Image != "" {
		imageName = cmd.Image
	}

	if imageName == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.example.com/your-username/model-name'")
	}

	// Look up the provider for the target registry
	p := providerReg.ForImage(imageName)
	if p == nil {
		return fmt.Errorf("no provider found for image '%s'", imageName)
	}

	pushOpts := provider.PushOptions{
		Image:      imageName,
		Config:     src.Config,
		ProjectDir: src.ProjectDir,
	}

	// Generate a push ID for annotations
	buildID, _ := uuid.NewV7()
	annotations := map[string]string{}
	if buildID.String() != "" {
		annotations["run.cog.push_id"] = buildID.String()
	}

	// Build the model
	resolver := model.NewResolver(dockerClient, regClient)
	m, err := resolver.Build(ctx, src, cmd.BuildOptions(imageName, annotations))
	if err != nil {
		_ = p.PostPush(ctx, pushOpts, err)
		return err
	}

	// Log weights info
	weights := m.WeightArtifacts()
	if len(weights) > 0 {
		console.Infof("\n%d weight artifact(s)", len(weights))
	}

	// Push the model
	console.Infof("\nPushing image '%s'...", m.ImageRef())
	pushErr := resolver.Push(ctx, m, model.PushOptions{})

	// PostPush: the provider handles formatting errors and showing success messages
	if err := p.PostPush(ctx, pushOpts, pushErr); err != nil {
		return err
	}

	if pushErr != nil {
		return fmt.Errorf("failed to push image: %w", pushErr)
	}

	return nil
}
