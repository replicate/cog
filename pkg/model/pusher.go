// pkg/model/pusher.go
package model

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// Pusher handles pushing a model to a registry.
type Pusher interface {
	Push(ctx context.Context, m *Model, opts PushOptions) error
}

// PushOptions configures push behavior.
type PushOptions struct {
	// ProjectDir is the base directory for resolving weight file paths.
	// Required when Model.ImageFormat == FormatBundle.
	ProjectDir string

	// Platform specifies the target platform for bundle indexes.
	// Default: linux/amd64
	Platform *Platform
}

// =============================================================================
// ImagePusher - pushes standalone images
// =============================================================================

// ImagePusher pushes standalone images using docker push.
type ImagePusher struct {
	docker command.Command
}

// NewImagePusher creates a new ImagePusher.
func NewImagePusher(docker command.Command) *ImagePusher {
	return &ImagePusher{docker: docker}
}

// Push pushes the model image to a registry.
func (p *ImagePusher) Push(ctx context.Context, m *Model, opts PushOptions) error {
	if m.Image == nil || m.Image.Reference == "" {
		return fmt.Errorf("model has no image reference")
	}
	return p.docker.Push(ctx, m.Image.Reference)
}

// =============================================================================
// BundlePusher - pushes OCI Index with image + weights
// =============================================================================

// BundlePusher pushes bundles (OCI Index with image + weights).
type BundlePusher struct {
	docker   command.Command
	registry registry.Client
}

// NewBundlePusher creates a new BundlePusher.
func NewBundlePusher(docker command.Command, reg registry.Client) *BundlePusher {
	return &BundlePusher{docker: docker, registry: reg}
}

// Push pushes the model as an OCI Index with weights artifact.
func (p *BundlePusher) Push(ctx context.Context, m *Model, opts PushOptions) error {
	if m.Image == nil || m.Image.Reference == "" {
		return fmt.Errorf("model has no image reference")
	}
	if m.WeightsManifest == nil {
		return fmt.Errorf("bundle format requires WeightsManifest")
	}
	if opts.ProjectDir == "" {
		return fmt.Errorf("bundle push requires ProjectDir for weight files")
	}

	// 1. Build weights artifact from manifest
	factory := NewIndexFactory()
	weightsArtifact, err := factory.BuildWeightsArtifactFromManifest(ctx, m.WeightsManifest, opts.ProjectDir)
	if err != nil {
		return fmt.Errorf("build weights artifact: %w", err)
	}

	// 2. Push model image to registry via docker
	if err := p.docker.Push(ctx, m.Image.Reference); err != nil {
		return fmt.Errorf("push model image: %w", err)
	}

	// 3. Fetch pushed image from registry to get v1.Image for index building
	modelImg, err := p.registry.GetImage(ctx, m.Image.Reference, nil)
	if err != nil {
		return fmt.Errorf("fetch pushed image: %w", err)
	}

	// 4. Build OCI index combining image + weights
	platform := opts.Platform
	if platform == nil {
		platform = &Platform{OS: "linux", Architecture: "amd64"}
	}
	idx, err := factory.BuildIndex(ctx, modelImg, weightsArtifact, platform)
	if err != nil {
		return fmt.Errorf("build OCI index: %w", err)
	}

	// 5. Push OCI index (overwrites the tag with the index)
	if err := p.registry.PushIndex(ctx, m.Image.Reference, idx); err != nil {
		return fmt.Errorf("push OCI index: %w", err)
	}

	return nil
}
