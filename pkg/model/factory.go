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
	// Build creates a Docker image from source and returns ImageArtifact metadata.
	// For dev mode (cog serve), set ExcludeSource=true in BuildOptions to skip
	// COPY . /src — the source directory is volume-mounted at runtime instead.
	Build(ctx context.Context, src *Source, opts BuildOptions) (*ImageArtifact, error)

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
func (f *DockerfileFactory) Build(ctx context.Context, src *Source, opts BuildOptions) (*ImageArtifact, error) {
	imageID, err := image.Build(
		ctx,
		src.Config,
		src.ProjectDir,
		opts.ImageName,
		src.ConfigFilename,
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
		opts.ExcludeSource,
		opts.Annotations,
		f.docker,
		f.registry,
	)
	if err != nil {
		return nil, err
	}

	return &ImageArtifact{
		Reference: opts.ImageName,
		Digest:    imageID,
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
