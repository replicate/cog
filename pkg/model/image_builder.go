package model

import (
	"context"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/docker/command"
)

// ImageBuilder builds ImageArtifact from ImageSpec.
// It delegates to a Factory for the actual docker build, then inspects
// the result to populate the artifact.
//
// NOTE: Resolver.Build() does not yet use ImageBuilder — it still calls
// factory.Build() + docker.Inspect() directly. ImageBuilder exists for
// symmetry with WeightBuilder and will replace the inline logic when
// the resolver is refactored to route all specs through builders.
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
// It delegates to the Factory for the docker build, inspects the result,
// and returns an ImageArtifact with the digest and reference.
func (b *ImageBuilder) Build(ctx context.Context, spec ArtifactSpec) (Artifact, error) {
	is, ok := spec.(*ImageSpec)
	if !ok {
		return nil, fmt.Errorf("image builder: expected *ImageSpec, got %T", spec)
	}

	// Build the image via the factory
	img, err := b.factory.Build(ctx, b.source, b.opts)
	if err != nil {
		return nil, fmt.Errorf("image build failed: %w", err)
	}

	// Inspect the built image to get the canonical digest.
	// Prefer digest (ID) for stable lookups, fall back to reference.
	inspectRef := img.Digest
	if inspectRef == "" {
		inspectRef = img.Reference
	}

	resp, err := b.docker.Inspect(ctx, inspectRef)
	if err != nil {
		return nil, fmt.Errorf("inspect built image: %w", err)
	}

	// Use the canonical ID from the inspect response
	digestStr := resp.ID
	digest, err := v1.NewHash(digestStr)
	if err != nil {
		return nil, fmt.Errorf("parse image digest %q: %w", digestStr, err)
	}

	desc := v1.Descriptor{Digest: digest}
	return NewImageArtifact(is.Name(), desc, img.Reference), nil
}
