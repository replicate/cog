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

	// OnFallback is called when OCI push fails and the push is about to fall
	// back to Docker push. This allows the caller to clean up any OCI-specific
	// progress display before Docker push starts its own output.
	OnFallback func()
}

// BundlePusher pushes an OCI Image Index containing a model image + its
// weight manifests. It pushes the image, HEAD-checks each weight
// manifest (which was pushed by `cog weights import`), then assembles
// the index from the descriptors.
type BundlePusher struct {
	imagePusher *ImagePusher
	registry    registry.Client
}

// NewBundlePusher creates a BundlePusher.
func NewBundlePusher(docker command.Command, reg registry.Client) *BundlePusher {
	return &BundlePusher{
		imagePusher: newImagePusher(docker, reg),
		registry:    reg,
	}
}

// Push pushes the model as an OCI Index. The image is pushed via
// ImagePusher. Weight manifests are verified via HEAD (they were
// pushed by `cog weights import`); if any are missing, the push
// fails with a message to re-run import.
func (p *BundlePusher) Push(ctx context.Context, m *Model, opts PushOptions) error {
	imgArtifact := m.GetImageArtifact()
	if imgArtifact == nil {
		return fmt.Errorf("no image artifact in model")
	}

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

	// HEAD the image and verify weight manifests concurrently.
	var imgDesc v1.Descriptor
	var weightDescs []v1.Descriptor

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var descErr error
		imgDesc, descErr = p.registry.GetDescriptor(gctx, imgArtifact.Reference)
		if descErr != nil {
			return fmt.Errorf("get image descriptor: %w", descErr)
		}
		return nil
	})
	g.Go(func() error {
		var verifyErr error
		weightDescs, verifyErr = p.verifyWeights(gctx, repo, m.Weights)
		return verifyErr
	})
	if err := g.Wait(); err != nil {
		return err
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
	for i, desc := range weightDescs {
		w := m.Weights[i]
		builder.AddWeightDescriptor(desc, w.Name, w.SetDigest, w.Size)
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

// verifyWeights HEAD-checks each weight manifest in the registry,
// returning descriptors in input order. Returns an error if any
// manifest is not found (the user needs to run `cog weights import`).
func (p *BundlePusher) verifyWeights(
	ctx context.Context,
	repo string,
	weights []Weight,
) ([]v1.Descriptor, error) {
	if len(weights) == 0 {
		return nil, nil
	}

	descs := make([]v1.Descriptor, len(weights))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(GetPushConcurrency())

	for i, w := range weights {
		g.Go(func() error {
			tag := WeightTag(w.Name, w.SetDigest)
			ref := repo + ":" + tag
			desc, err := p.registry.GetDescriptor(ctx, ref)
			if err != nil {
				return fmt.Errorf(
					"weight %q not found in registry (%s); run 'cog weights import' to push weights first: %w",
					w.Name, ref, err,
				)
			}
			descs[i] = desc
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return descs, nil
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
