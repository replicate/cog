package model

import (
	"context"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/registry"
)

// Factory is the build backend interface.
// Different implementations handle different build strategies.
type Factory interface {
	// Build creates a Docker image from source and returns Image metadata.
	Build(ctx context.Context, src *Source, opts BuildOptions) (*Image, error)

	// Name returns the factory name for logging/debugging.
	Name() string
}

// DockerfileFactory wraps existing Dockerfile-based build.
type DockerfileFactory struct {
	docker   command.Command
	registry registry.Client
}

// NewDockerfileFactory creates a Factory that uses the existing Dockerfile-based build.
func NewDockerfileFactory(docker command.Command, registry registry.Client) Factory {
	return &DockerfileFactory{docker: docker, registry: registry}
}

// Name returns the factory name.
func (f *DockerfileFactory) Name() string {
	return "dockerfile"
}

// Build delegates to the existing image.Build() function.
func (f *DockerfileFactory) Build(ctx context.Context, src *Source, opts BuildOptions) (*Image, error) {
	err := image.Build(
		ctx,
		src.Config,
		src.ProjectDir,
		opts.ImageName,
		opts.Secrets,
		opts.NoCache,
		opts.SeparateWeights,
		opts.UseCudaBaseImage,
		opts.ProgressOutput,
		opts.SchemaFile,
		opts.DockerfileFile,
		opts.UseCogBaseImage,
		opts.Strip,
		opts.Precompile,
		opts.Fast,
		opts.Annotations,
		opts.LocalImage,
		f.docker,
		f.registry,
		opts.PipelinesImage,
	)
	if err != nil {
		return nil, err
	}

	return &Image{
		Reference: opts.ImageName,
		Source:    ImageSourceBuild,
	}, nil
}

// DefaultFactory returns a Factory based on environment variables.
// It checks COG_BUILDER and COGPACK to select the appropriate backend.
//
// TODO: When FrontendFactory is implemented, check COG_BUILDER env var.
// TODO: When CogpacksFactory is implemented, check COGPACK env var.
func DefaultFactory(docker command.Command, registry registry.Client) Factory {
	return NewDockerfileFactory(docker, registry)
}
