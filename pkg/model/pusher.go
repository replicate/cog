// pkg/model/pusher.go
package model

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

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
// It orchestrates OCIImagePusher and WeightPusher, then assembles the OCI index
// from the pushed manifest descriptors.
type BundlePusher struct {
	ociPusher    *OCIImagePusher
	imagePusher  *ImagePusher
	weightPusher *WeightPusher
	registry     registry.Client
}

// NewBundlePusher creates a new BundlePusher.
func NewBundlePusher(docker command.Command, reg registry.Client, ociPusher *OCIImagePusher) *BundlePusher {
	return &BundlePusher{
		ociPusher:    ociPusher,
		imagePusher:  NewImagePusher(docker),
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
		weightResults, err = p.pushWeightsConcurrently(ctx, repo, weightArtifacts)
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

// pushContainerImage pushes the container image using the OCI chunked push path.
// Falls back to legacy Docker push if OCI push fails (except on auth/context errors).
func (p *BundlePusher) pushContainerImage(ctx context.Context, imgArtifact *ImageArtifact) error {
	return pushImageWithFallback(ctx, p.ociPusher, p.imagePusher, imgArtifact)
}

// pushWeightsConcurrently pushes all weight artifacts and returns their results.
// If any weight push fails, remaining pushes are canceled and the first error is returned.
func (p *BundlePusher) pushWeightsConcurrently(ctx context.Context, repo string, weights []*WeightArtifact) ([]*WeightPushResult, error) {
	// Create cancellable context so we can stop remaining pushes on first error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type indexedResult struct {
		index  int
		result *WeightPushResult
		err    error
	}

	results := make(chan indexedResult, len(weights))
	var wg sync.WaitGroup

	for i, wa := range weights {
		wg.Add(1)
		go func(idx int, artifact *WeightArtifact) {
			defer wg.Done()
			result, err := p.weightPusher.Push(ctx, repo, artifact)
			results <- indexedResult{index: idx, result: result, err: err}
		}(i, wa)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Drain all results, canceling on first error
	ordered := make([]*WeightPushResult, len(weights))
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("push weight %q: %w", weights[r.index].Name(), r.err)
			cancel() // Signal remaining goroutines to stop
		}
		if r.err == nil {
			ordered[r.index] = r.result
		}
	}
	if firstErr != nil {
		return nil, firstErr
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
