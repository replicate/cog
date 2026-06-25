package model

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
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
// weight manifests. It pushes the image under a cog-image.* tag,
// HEAD-checks each weight manifest (which was pushed by `cog weights
// import`), then assembles and pushes the index under the model tag.
//
// Push mutates the local Docker daemon: it adds a "{repo}:cog-image.*"
// tag to the existing image before pushing and removes that tag on
// successful return. Callers reusing BundlePusher outside the
// build-then-push flow should be aware of this side effect.
type BundlePusher struct {
	docker      command.Command
	imagePusher *ImagePusher
	registry    registry.Client
}

// NewBundlePusher creates a BundlePusher.
func NewBundlePusher(docker command.Command, reg registry.Client) *BundlePusher {
	return &BundlePusher{
		docker:      docker,
		imagePusher: newImagePusher(docker, reg),
		registry:    reg,
	}
}

// Push pushes the model as an OCI Index and returns an enriched copy
// of the Model with all registry references populated.
//
// Algorithm:
//
//  1. Verify every weight manifest exists at repo@digest *before*
//     anything else; missing weights fail fast so we don't leave an
//     orphan image in the registry.
//  2. Re-tag the locally-built image to "{repo}:cog-image.{tag}" so
//     the image manifest lands at a stable, namespaced tag in the
//     registry, independent of whatever tag the model index will
//     carry. Push from that tag.
//  3. HEAD the image to capture its registry-side digest.
//  4. Build and push the OCI index at the model ref; capture its
//     descriptor locally from the index bytes (content-addressed —
//     no second registry round-trip needed).
//  5. Return a Model with Ref pinned to the index digest, the image
//     artifact's Reference rewritten to repo@digest, and each weight
//     enriched with its registry reference and tag.
//
// The caller's Model.Ref drives the destination. Models loaded by
// Inspect/Pull have a Format derived from manifest shape and leave
// Ref nil — those are not push-ready; only models produced by
// Resolver.Build carry the resolved Ref this method needs.
func (p *BundlePusher) Push(ctx context.Context, m *Model, opts PushOptions) (*Model, error) {
	imgArtifact := m.GetImageArtifact()
	if imgArtifact == nil {
		return nil, fmt.Errorf("no image artifact in model")
	}
	if m.Ref == nil {
		return nil, fmt.Errorf("bundle push requires Model.Ref to be set")
	}
	if m.Ref.Tag == "" {
		// ResolveModelRef pairs Digest with empty Tag; a digest-pinned
		// ref describes an already-published model and can't be a push
		// destination.
		return nil, fmt.Errorf("cannot push to digest-pinned ref %q: model push requires a tag", m.Ref.String())
	}

	repo := m.Ref.Repository()

	// Verify weight manifests exist before pushing the image.
	weightDescs, err := p.verifyWeights(ctx, repo, m.Weights)
	if err != nil {
		return nil, err
	}

	// Pair the image tag with the model tag (e.g. v2 → cog-image.v2,
	// 20260512T...Z → cog-image.20260512T...Z) so the two manifests
	// are visibly related when browsing the registry.
	imageRef := repo + ":" + ImageTag(m.Ref.Tag)

	// Re-tag the locally-built image at the cog-image ref so push
	// (whether OCI or docker push) targets the right place in the
	// registry. The build-path tag (imgArtifact.Reference) still
	// points at the same image, so the deferred RemoveImage just
	// untags the cog-image alias.
	//
	// Skip the tag+cleanup pair when the build already produced an
	// image at the cog-image ref (e.g. a caller pre-named their
	// image): removing it would delete the only local tag and the
	// underlying image with it.
	if imgArtifact.Reference != imageRef {
		if err := p.docker.Tag(ctx, imgArtifact.Reference, imageRef); err != nil {
			return nil, err
		}
		defer func() {
			if err := p.docker.RemoveImage(context.WithoutCancel(ctx), imageRef); err != nil {
				console.Debugf("removing local cog-image tag %q: %v", imageRef, err)
			}
		}()
	}

	// Wholesale copy preserves the unexported descriptor that a
	// constructor can't reach; overwrite only the reference.
	imageCopy := *imgArtifact
	imageCopy.Reference = imageRef
	pushedImage := &imageCopy

	var imagePushOpts []ImagePushOption
	if opts.ImageProgressFn != nil {
		imagePushOpts = append(imagePushOpts, WithProgressFn(opts.ImageProgressFn))
	}
	if opts.OnFallback != nil {
		imagePushOpts = append(imagePushOpts, WithOnFallback(opts.OnFallback))
	}
	if err := p.imagePusher.Push(ctx, pushedImage, imagePushOpts...); err != nil {
		return nil, fmt.Errorf("push image %q: %w", imageRef, err)
	}

	imgDesc, err := p.registry.GetDescriptor(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("get image descriptor: %w", err)
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
		return nil, fmt.Errorf("build OCI index: %w", err)
	}

	if err := p.registry.PushIndex(ctx, m.Ref.String(), idx); err != nil {
		return nil, fmt.Errorf("push OCI index: %w", err)
	}

	// The index is content-addressed: its bytes are exactly what the
	// registry stored, so idx.Digest() returns the registry-side
	// digest without a HEAD round-trip.
	indexDigest, err := idx.Digest()
	if err != nil {
		return nil, fmt.Errorf("compute index digest: %w", err)
	}

	return enrichBundleModel(m, repo, imgArtifact, imgDesc, indexDigest.String()), nil
}

// enrichBundleModel returns a shallow copy of m with Ref pinned to
// the index digest, the image artifact's Reference rewritten to its
// repo@digest form, and each weight enriched with its registry
// reference and tag.
//
// imgArtifact is the original artifact returned by GetImageArtifact;
// it identifies which entry in m.Artifacts to substitute, by pointer
// identity.
func enrichBundleModel(
	m *Model,
	repo string,
	imgArtifact *ImageArtifact,
	imgDesc v1.Descriptor,
	indexDigest string,
) *Model {
	out := *m
	out.Ref = &ResolvedRef{
		Registry: m.Ref.Registry,
		Repo:     m.Ref.Repo,
		Tag:      m.Ref.Tag,
		Digest:   indexDigest,
	}

	enrichedImage := imgArtifact.WithDigest(repo, imgDesc)
	out.Image, out.Artifacts = replaceImageArtifact(imgArtifact, m.Artifacts, enrichedImage)

	if len(m.Weights) > 0 {
		weights := make([]Weight, len(m.Weights))
		for i, w := range m.Weights {
			w.Reference = repo + "@" + w.Digest
			w.Tag = WeightTag(w.Name, w.SetDigest)
			weights[i] = w
		}
		out.Weights = weights
	}

	return &out
}

// verifyWeights HEAD-checks each weight manifest by digest. Tags are
// mutable; resolving repo@digest forces the registry to return the
// exact manifest the lockfile recorded, and we cross-check the
// returned digest in case of a non-content-addressed proxy.
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
			if w.Digest == "" {
				return fmt.Errorf("weight %q: missing manifest digest in lockfile; re-run 'cog weights import'", w.Name)
			}
			ref := repo + "@" + w.Digest
			desc, err := p.registry.GetDescriptor(ctx, ref)
			if err != nil {
				return fmt.Errorf(
					"weight %q not found in registry (%s); run 'cog weights import' to push weights first: %w",
					w.Name, ref, err,
				)
			}
			if desc.Digest.String() != w.Digest {
				return fmt.Errorf("weight %q: registry returned digest %s for requested %s",
					w.Name, desc.Digest.String(), w.Digest)
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

// replaceImageArtifact returns a new Image and Artifacts slice with
// enriched substituted for old by pointer identity. Callers pass the
// artifact pointer they actually used (typically the result of
// Model.GetImageArtifact), so the match doesn't depend on the
// Model.Image-vs-Artifacts[0] invariant.
func replaceImageArtifact(old *ImageArtifact, artifacts []Artifact, enriched *ImageArtifact) (*ImageArtifact, []Artifact) {
	if len(artifacts) == 0 {
		return enriched, nil
	}
	out := make([]Artifact, len(artifacts))
	for i, a := range artifacts {
		if ia, ok := a.(*ImageArtifact); ok && ia == old {
			out[i] = enriched
			continue
		}
		out[i] = a
	}
	return enriched, out
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
