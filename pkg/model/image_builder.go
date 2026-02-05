package model

import (
	"context"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/docker/command"
)

// ImageBuilder builds an ImageArtifact from an ImageSpec.
// It delegates to a Factory for the docker build, inspects the result
// to populate labels and the canonical digest, and returns a fully
// populated ImageArtifact.
type ImageBuilder struct {
	factory Factory
	docker  command.Command
	source  *Source
	opts    BuildOptions
}

// NewImageBuilder creates an ImageBuilder.
func NewImageBuilder(factory Factory, docker command.Command, source *Source, opts BuildOptions) *ImageBuilder {
	return &ImageBuilder{
		factory: factory,
		docker:  docker,
		source:  source,
		opts:    opts,
	}
}

// Build builds an ImageArtifact from an ImageSpec.
// It delegates to the Factory for the docker build, inspects the result
// to populate labels and the canonical digest, and returns a fully
// populated ImageArtifact.
func (b *ImageBuilder) Build(ctx context.Context, spec ArtifactSpec) (Artifact, error) {
	is, ok := spec.(*ImageSpec)
	if !ok {
		return nil, fmt.Errorf("image builder: expected *ImageSpec, got %T", spec)
	}

	// Build the image via the factory (returns partially populated ImageArtifact)
	img, err := b.factory.Build(ctx, b.source, b.opts)
	if err != nil {
		return nil, fmt.Errorf("image build failed: %w", err)
	}

	// Inspect the built image to get labels and canonical digest.
	// Prefer digest (ID) for stable lookups, fall back to reference.
	inspectRef := img.Digest
	if inspectRef == "" {
		inspectRef = img.Reference
	}

	resp, err := b.docker.Inspect(ctx, inspectRef)
	if err != nil {
		return nil, fmt.Errorf("inspect built image: %w", err)
	}

	// Populate the artifact with inspect results
	img.name = is.Name()
	img.Labels = resp.Config.Labels
	img.Digest = resp.ID
	img.Source = ImageSourceBuild

	digest, err := v1.NewHash(resp.ID)
	if err != nil {
		return nil, fmt.Errorf("parse image digest %q: %w", resp.ID, err)
	}
	img.descriptor = v1.Descriptor{Digest: digest}

	return img, nil
}
