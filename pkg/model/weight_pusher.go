package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/registry"
)

// WeightPusher pushes a WeightArtifact as a v1 multi-layer OCI artifact.
// Each tar layer is uploaded via registry.WriteLayer (which supports
// multipart uploads, progress, and retry), followed by the manifest via
// registry.PushImage. Layers upload concurrently, bounded by
// GetPushConcurrency.
type WeightPusher struct {
	registry registry.Client
}

// NewWeightPusher creates a new WeightPusher.
func NewWeightPusher(reg registry.Client) *WeightPusher {
	return &WeightPusher{registry: reg}
}

// WeightPushOptions configures a weight push.
type WeightPushOptions struct {
	// Concurrency is the maximum number of layers to upload in parallel.
	// If <= 0, GetPushConcurrency() is used.
	Concurrency int
	// Tag overrides the manifest tag. Defaults to
	// WeightTag(artifact.Name, tagSeed) where tagSeed is the set digest.
	Tag string
	// ProgressFn is an optional callback for per-layer upload progress.
	ProgressFn func(WeightLayerProgress)
	// RetryFn is an optional retry callback, invoked per-layer.
	// Return false to abort the retry.
	RetryFn func(WeightRetryEvent) bool
}

// WeightLayerProgress reports per-layer progress for a weight push. When
// dispatched by BundlePusher, WeightName identifies which artifact the
// layer belongs to; the per-weight WeightPusher.Push path leaves it empty.
type WeightLayerProgress struct {
	WeightName  string
	LayerDigest string
	Complete    int64
	Total       int64
}

// WeightRetryEvent reports a retry attempt for a weight layer upload.
type WeightRetryEvent struct {
	// Name identifies which layer is being retried. It combines the weight
	// name and layer digest, e.g. "z-image-turbo layer sha256:abc…".
	Name        string
	Attempt     int
	MaxAttempts int
	Err         error
	NextRetryIn time.Duration
}

// WeightPushResult describes a successful weight push.
type WeightPushResult struct {
	// Ref is the full image reference the manifest was pushed to
	// (e.g. "registry/repo:weights-name-abc123").
	Ref string
	// Descriptor is the OCI descriptor for the pushed manifest.
	Descriptor v1.Descriptor
}

// Push pushes a WeightArtifact to the registry as a v1 OCI weight manifest.
// Layers upload concurrently; the manifest goes up last.
//
// The caller owns the tar files referenced by artifact.Layers[i].TarPath —
// Push reads them but does not delete them, so the caller can push the same
// artifact to multiple registries or retry after a transient failure. On
// layer-upload failure the manifest is not attempted, but any
// already-uploaded layers remain in the registry (garbage-collectable).
func (p *WeightPusher) Push(ctx context.Context, repo string, artifact *WeightArtifact, opts ...WeightPushOptions) (*WeightPushResult, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if len(artifact.Layers) == 0 {
		return nil, fmt.Errorf("weight %q has no layers", artifact.Name())
	}

	var opt WeightPushOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	img := artifact.Manifest()
	if img == nil {
		// Rebuild if the artifact was constructed without a cached manifest
		// (e.g. in tests via NewWeightArtifact).
		var err error
		img, err = BuildWeightManifestV1(artifact.Entry, artifact.Layers)
		if err != nil {
			return nil, fmt.Errorf("build weight manifest: %w", err)
		}
	}

	if err := p.pushLayersConcurrently(ctx, repo, artifact.Name(), artifact.Layers, opt); err != nil {
		return nil, fmt.Errorf("push weight layers: %w", err)
	}

	tag := opt.Tag
	if tag == "" {
		tag = WeightTag(artifact.Name(), artifact.Entry.SetDigest)
	}
	ref := repo + ":" + tag
	if err := p.registry.PushImage(ctx, ref, img); err != nil {
		return nil, fmt.Errorf("push weight manifest (%s): %w", tag, err)
	}

	desc, err := descriptorFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("compute manifest descriptor: %w", err)
	}
	return &WeightPushResult{Ref: ref, Descriptor: desc}, nil
}

// pushLayersConcurrently pushes all layers using bounded concurrency,
// returning the first error (if any).
func (p *WeightPusher) pushLayersConcurrently(
	ctx context.Context,
	repo, weightName string,
	layers []PackedLayer,
	opt WeightPushOptions,
) error {
	concurrency := opt.Concurrency
	if concurrency <= 0 {
		concurrency = GetPushConcurrency()
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, lr := range layers {
		g.Go(func() error {
			return p.pushSingleLayer(ctx, repo, weightName, lr, opt)
		})
	}

	return g.Wait()
}

// pushSingleLayer pushes a single tar layer via registry.WriteLayer, wiring
// up progress and retry callbacks if configured.
func (p *WeightPusher) pushSingleLayer(
	ctx context.Context,
	repo, weightName string,
	lr PackedLayer,
	opt WeightPushOptions,
) error {
	layer := newFileLayer(lr)
	digestStr := lr.Digest.String()

	var onProgress func(v1.Update)
	if opt.ProgressFn != nil {
		onProgress = func(update v1.Update) {
			opt.ProgressFn(WeightLayerProgress{
				WeightName:  weightName,
				LayerDigest: digestStr,
				Complete:    update.Complete,
				Total:       update.Total,
			})
		}
	}

	var retryConfig *registry.RetryConfig
	if opt.RetryFn != nil {
		retryConfig = &registry.RetryConfig{
			OnRetry: func(event registry.RetryEvent) bool {
				return opt.RetryFn(WeightRetryEvent{
					Name:        fmt.Sprintf("%s layer %s", weightName, digestStr),
					Attempt:     event.Attempt,
					MaxAttempts: event.MaxAttempts,
					Err:         event.Err,
					NextRetryIn: event.NextRetryIn,
				})
			},
		}
	}

	err := writeLayerWithProgress(ctx, p.registry, registry.WriteLayerOptions{
		Repo:  repo,
		Layer: layer,
		Retry: retryConfig,
	}, onProgress)
	if err != nil {
		return fmt.Errorf("push layer %s: %w", digestStr, err)
	}
	return nil
}

// descriptorFromImage computes the v1.Descriptor for a built image manifest.
func descriptorFromImage(img v1.Image) (v1.Descriptor, error) {
	digest, err := img.Digest()
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("get digest: %w", err)
	}

	rawManifest, err := img.RawManifest()
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("get raw manifest: %w", err)
	}

	mediaType, err := img.MediaType()
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("get media type: %w", err)
	}

	return v1.Descriptor{
		MediaType: mediaType,
		Size:      int64(len(rawManifest)),
		Digest:    digest,
	}, nil
}

const weightTagPrefix = "weights-"

// WeightTag returns the tag for a weight manifest combining name and the
// short prefix of a digest. digest is "sha256:…"; the 12 hex chars after
// the algorithm prefix are used. Falls back to "weights-<name>" if digest
// is empty or missing the algorithm prefix.
func WeightTag(name, digest string) string {
	short := ShortDigest(digest)
	if short == "" {
		return weightTagPrefix + name
	}
	return weightTagPrefix + name + "-" + short
}

// ShortDigest returns the 12-hex-char prefix of a "sha256:…" digest, or the
// empty string if the input is empty or has no algorithm prefix.
func ShortDigest(digest string) string {
	_, hex, ok := strings.Cut(digest, ":")
	if !ok || hex == "" {
		return ""
	}
	if len(hex) > 12 {
		return hex[:12]
	}
	return hex
}
