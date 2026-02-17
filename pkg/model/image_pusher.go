package model

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/docker/command"
)

// ImagePusher pushes container images using docker push.
type ImagePusher struct {
	docker command.Command
}

// NewImagePusher creates a new ImagePusher.
func NewImagePusher(docker command.Command) *ImagePusher {
	return &ImagePusher{docker: docker}
}

// PushArtifact pushes a single image artifact by reference.
// This is the artifact-aware method used by BundlePusher in Phase 4.
func (p *ImagePusher) PushArtifact(ctx context.Context, artifact *ImageArtifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact is nil")
	}
	if artifact.Reference == "" {
		return fmt.Errorf("image has no reference")
	}
	return p.docker.Push(ctx, artifact.Reference)
}

// Push pushes the model image to a registry.
// This implements the Pusher interface for backwards compatibility with Resolver.Push().
func (p *ImagePusher) Push(ctx context.Context, m *Model, opts PushOptions) error {
	return p.PushArtifact(ctx, m.Image)
}
