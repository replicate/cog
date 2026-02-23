// pkg/model/pusher.go
package model

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/registry"
)

// PushOptions configures push behavior.
type PushOptions struct {
	// ProjectDir is the base directory for resolving weight file paths.
	//
	// Deprecated: Artifacts carry their own file paths.
	ProjectDir string

	// FilePaths maps weight name identifiers to their file paths.
	//
	// Deprecated: Use Model.Artifacts instead — WeightArtifact carries FilePath.
	FilePaths map[string]string

	// Platform specifies the target platform for bundle indexes.
	// Default: linux/amd64
	Platform *Platform
}

// =============================================================================
// BundlePusher - pushes OCI Index with image + weights
// =============================================================================

// BundlePusher pushes bundles (OCI Index with image + weight artifacts).
// It orchestrates ImagePusher and WeightPusher, then assembles the OCI index
// from the pushed manifest descriptors.
type BundlePusher struct {
	imagePusher  *ImagePusher
	weightPusher *WeightPusher
	registry     registry.Client
}

// NewBundlePusher creates a new BundlePusher.
func NewBundlePusher(imagePusher *ImagePusher, reg registry.Client) *BundlePusher {
	return &BundlePusher{
		imagePusher:  imagePusher,
		weightPusher: NewWeightPusher(reg),
		registry:     reg,
	}
}

// Push pushes the model as an OCI Index with weight artifacts.
// It reads Model.Artifacts to find the image and weight artifacts to push.
func (p *BundlePusher) Push(ctx context.Context, m *Model, opts PushOptions) error {
	// Extract artifacts from model
	imgArtifact := m.GetImageArtifact()
	if imgArtifact == nil {
		return fmt.Errorf("no image artifact in model")
	}

	weightArtifacts := m.WeightArtifacts()

	// Derive repo from image reference (strip tag/digest for weight pushes)
	repo := repoFromReference(imgArtifact.Reference)

	// 1. Push image via OCI chunked push (falls back to Docker push on error)
	if err := p.pushContainerImage(ctx, imgArtifact); err != nil {
		return fmt.Errorf("push image %q: %w", imgArtifact.Reference, err)
	}

	// 2. Get image manifest descriptor (lightweight HEAD request)
	imgDesc, err := p.registry.GetDescriptor(ctx, imgArtifact.Reference)
	if err != nil {
		return fmt.Errorf("get image descriptor: %w", err)
	}

	// 3. Push weight artifacts concurrently (if any)
	var weightResults []*WeightPushResult
	if len(weightArtifacts) > 0 {
		weightResults, err = p.pushWeights(ctx, repo, weightArtifacts)
		if err != nil {
			return err
		}
	}

	// 4. Build OCI index from pushed descriptors
	platform := opts.Platform
	if platform == nil {
		platform = &Platform{OS: "linux", Architecture: "amd64"}
	}

	builder := NewIndexBuilder()
	builder.SetImageDescriptor(imgDesc, &v1.Platform{
		OS:           platform.OS,
		Architecture: platform.Architecture,
		Variant:      platform.Variant,
	})
	for i, wr := range weightResults {
		builder.AddWeightDescriptor(wr.Descriptor, imgDesc.Digest.String(),
			weightArtifacts[i].Name(), weightArtifacts[i].Target)
	}

	idx, err := builder.BuildFromDescriptors()
	if err != nil {
		return fmt.Errorf("build OCI index: %w", err)
	}

	// 5. Push OCI index (overwrites the tag with the index)
	if err := p.registry.PushIndex(ctx, imgArtifact.Reference, idx); err != nil {
		return fmt.Errorf("push OCI index: %w", err)
	}

	return nil
}

// pushContainerImage pushes the container image via ImagePusher (OCI chunked
// push with Docker fallback).
func (p *BundlePusher) pushContainerImage(ctx context.Context, imgArtifact *ImageArtifact) error {
	return p.imagePusher.PushArtifact(ctx, imgArtifact)
}

// pushWeights pushes all weight artifacts concurrently (bounded by GetPushConcurrency)
// and returns their results in the same order as the input slice.
// If any weight push fails, remaining pushes are canceled and the first error is returned.
func (p *BundlePusher) pushWeights(ctx context.Context, repo string, weights []*WeightArtifact) ([]*WeightPushResult, error) {
	ordered := make([]*WeightPushResult, len(weights))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(GetPushConcurrency())

	for i, wa := range weights {
		g.Go(func() error {
			result, err := p.weightPusher.Push(ctx, repo, wa)
			if err != nil {
				return fmt.Errorf("push weight %q: %w", wa.Name(), err)
			}
			ordered[i] = result
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return ordered, nil
}

// repoFromReference extracts the repository (without tag or digest) from an image reference.
// "r8.im/user/model:latest" -> "r8.im/user/model"
// "r8.im/user/model@sha256:abc" -> "r8.im/user/model"
// "localhost:5000/model:latest" -> "localhost:5000/model"
func repoFromReference(ref string) string {
	parsed, err := name.ParseReference(ref, name.Insecure)
	if err != nil {
		return ref // fallback: return as-is if unparseable
	}
	return parsed.Context().String()
}
