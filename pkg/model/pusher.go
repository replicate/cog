package model

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// PushOptions configures push behavior.
type PushOptions struct {
	// Platform specifies the target platform for bundle indexes.
	// Default: linux/amd64
	Platform *Platform

	// ImageProgressFn is an optional callback for reporting push progress.
	// It receives both phase transitions (Phase set, byte fields zero) and
	// per-layer byte progress (Phase empty, Complete/Total set).
	ImageProgressFn func(PushProgress)

	// WeightProgressFn is an optional callback for per-weight-layer upload
	// progress. WeightLayerProgress.WeightName identifies which artifact
	// the update belongs to.
	WeightProgressFn func(WeightLayerProgress)

	// OnFallback is called when OCI push fails and the push is about to fall
	// back to Docker push. This allows the caller to clean up any OCI-specific
	// progress display before Docker push starts its own output.
	OnFallback func()
}

// BundlePusher pushes an OCI Image Index containing a model image + its
// weight artifacts. It orchestrates ImagePusher and WeightPusher, then
// assembles the index from the pushed manifest descriptors.
type BundlePusher struct {
	imagePusher  *ImagePusher
	weightPusher *WeightPusher
	registry     registry.Client
}

// NewBundlePusher creates a BundlePusher.
func NewBundlePusher(docker command.Command, reg registry.Client) *BundlePusher {
	return &BundlePusher{
		imagePusher:  newImagePusher(docker, reg),
		weightPusher: NewWeightPusher(reg),
		registry:     reg,
	}
}

// Push pushes the model as an OCI Index with its weight artifacts.
func (p *BundlePusher) Push(ctx context.Context, m *Model, opts PushOptions) error {
	imgArtifact := m.GetImageArtifact()
	if imgArtifact == nil {
		return fmt.Errorf("no image artifact in model")
	}

	weightArtifacts := m.WeightArtifacts()
	repo := repoFromReference(imgArtifact.Reference)

	var imagePushOpts []ImagePushOption
	if opts.ImageProgressFn != nil {
		imagePushOpts = append(imagePushOpts, WithProgressFn(opts.ImageProgressFn))
	}
	if opts.OnFallback != nil {
		imagePushOpts = append(imagePushOpts, WithOnFallback(opts.OnFallback))
	}
	if err := p.imagePusher.Push(ctx, imgArtifact, imagePushOpts...); err != nil {
		return fmt.Errorf("push image %q: %w", imgArtifact.Reference, err)
	}

	// Lightweight HEAD, to anchor the OCI index entry and the
	// run.cog.reference.digest annotation on each weight manifest.
	imgDesc, err := p.registry.GetDescriptor(ctx, imgArtifact.Reference)
	if err != nil {
		return fmt.Errorf("get image descriptor: %w", err)
	}

	var weightResults []*WeightPushResult
	if len(weightArtifacts) > 0 {
		weightResults, err = p.pushWeights(ctx, repo, weightArtifacts, opts.WeightProgressFn)
		if err != nil {
			return err
		}
	}

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
		wa := weightArtifacts[i]
		builder.AddWeightDescriptor(wr.Descriptor,
			wa.Entry.Name, wa.Entry.SetDigest, wa.Entry.Size)
	}

	idx, err := builder.BuildFromDescriptors()
	if err != nil {
		return fmt.Errorf("build OCI index: %w", err)
	}

	// Overwrites the tag with the index.
	if err := p.registry.PushIndex(ctx, imgArtifact.Reference, idx); err != nil {
		return fmt.Errorf("push OCI index: %w", err)
	}

	return nil
}

// pushWeights pushes all weight artifacts concurrently (bounded by
// GetPushConcurrency) and returns their results in input order.
func (p *BundlePusher) pushWeights(
	ctx context.Context,
	repo string,
	weights []*WeightArtifact,
	progressFn func(WeightLayerProgress),
) ([]*WeightPushResult, error) {
	ordered := make([]*WeightPushResult, len(weights))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(GetPushConcurrency())

	for i, wa := range weights {
		g.Go(func() error {
			result, err := p.weightPusher.Push(ctx, repo, wa, WeightPushOptions{
				ProgressFn: progressFn,
			})
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

// repoFromReference extracts the repository (without tag or digest) from an
// image reference. "r8.im/user/model:latest" -> "r8.im/user/model".
func repoFromReference(ref string) string {
	parsed, err := name.ParseReference(ref, name.Insecure)
	if err != nil {
		return ref // fallback: return as-is if unparseable
	}
	return parsed.Context().String()
}
