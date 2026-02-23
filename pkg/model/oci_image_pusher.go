package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/oci"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

const (
	// DefaultPushConcurrency is the default number of concurrent layer uploads.
	DefaultPushConcurrency = 4

	// envPushConcurrency overrides the default concurrency.
	envPushConcurrency = "COG_PUSH_CONCURRENCY"
)

// OCIImagePusher pushes container images to a registry using the OCI Distribution API
// with chunked uploads. It exports images from the Docker daemon to OCI layout,
// then pushes layers concurrently using the registry client's WriteLayer method.
type OCIImagePusher struct {
	registry  registry.Client
	imageSave oci.ImageSaveFunc
}

// NewOCIImagePusher creates a new OCIImagePusher.
//
// imageSave should export a Docker image as a tar stream. Typically this wraps
// the Docker SDK's client.ImageSave method.
func NewOCIImagePusher(reg registry.Client, imageSave oci.ImageSaveFunc) *OCIImagePusher {
	return &OCIImagePusher{
		registry:  reg,
		imageSave: imageSave,
	}
}

// ImagePushProgress reports progress for a layer upload.
type ImagePushProgress struct {
	// LayerDigest identifies which layer this progress is for.
	LayerDigest string
	// Complete is the number of bytes uploaded so far.
	Complete int64
	// Total is the total number of bytes to upload.
	Total int64
}

// ImagePushOptions configures push behavior for OCIImagePusher.
type ImagePushOptions struct {
	// ProgressFn is an optional callback for reporting per-layer upload progress.
	ProgressFn func(ImagePushProgress)
}

// Push exports the image from Docker daemon to OCI layout, then pushes all layers,
// config, and manifest to the registry using chunked uploads.
//
// The image reference (e.g., "r8.im/user/model:latest") is used both to load
// the image from Docker and as the destination in the registry.
func (p *OCIImagePusher) Push(ctx context.Context, imageRef string, opts ...ImagePushOptions) error {
	var opt ImagePushOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	// Export from Docker daemon to OCI layout
	layoutDir, img, err := oci.ExportOCILayout(ctx, imageRef, p.imageSave)
	if err != nil {
		return fmt.Errorf("export OCI layout: %w", err)
	}
	defer func() {
		console.Debugf("Cleaning up OCI layout directory: %s", layoutDir)
		_ = os.RemoveAll(layoutDir)
	}()

	return p.pushImage(ctx, imageRef, img, opt)
}

// PushFromLayout pushes an already-exported OCI layout image to the registry.
// This is used when the OCI layout has already been created (e.g., during build).
func (p *OCIImagePusher) PushFromLayout(ctx context.Context, imageRef string, layoutPath string, opts ...ImagePushOptions) error {
	var opt ImagePushOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	img, err := oci.LoadOCILayoutImage(layoutPath)
	if err != nil {
		return fmt.Errorf("load OCI layout: %w", err)
	}

	return p.pushImage(ctx, imageRef, img, opt)
}

// pushImage pushes a v1.Image (layers, config, manifest) to the registry.
func (p *OCIImagePusher) pushImage(ctx context.Context, imageRef string, img v1.Image, opt ImagePushOptions) error {
	// Extract repo from reference for WriteLayer calls
	repo := repoFromReference(imageRef)

	// Push layers concurrently
	if err := p.pushLayers(ctx, repo, img, opt); err != nil {
		return fmt.Errorf("push layers: %w", err)
	}

	// Push config blob
	if err := p.pushConfig(ctx, repo, img); err != nil {
		return fmt.Errorf("push config: %w", err)
	}

	// Push manifest
	console.Debugf("Pushing image manifest for %s", imageRef)
	if err := p.registry.PushImage(ctx, imageRef, img); err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}

	return nil
}

// pushLayers pushes all image layers concurrently using the registry client's
// WriteLayer method, which handles chunked uploads, retry, and progress reporting.
func (p *OCIImagePusher) pushLayers(ctx context.Context, repo string, img v1.Image, opt ImagePushOptions) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("get image layers: %w", err)
	}

	if len(layers) == 0 {
		return nil
	}

	concurrency := getPushConcurrency()
	console.Debugf("Pushing %d layers with concurrency %d", len(layers), concurrency)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, layer := range layers {
		g.Go(func() error {
			return p.pushLayer(ctx, repo, layer, opt)
		})
	}

	return g.Wait()
}

// pushLayer pushes a single layer with progress reporting.
func (p *OCIImagePusher) pushLayer(ctx context.Context, repo string, layer v1.Layer, opt ImagePushOptions) error {
	digest, err := layer.Digest()
	if err != nil {
		return fmt.Errorf("get layer digest: %w", err)
	}

	size, err := layer.Size()
	if err != nil {
		return fmt.Errorf("get layer size: %w", err)
	}

	console.Debugf("Pushing layer %s (%d bytes)", digest, size)

	// Set up progress channel if callback is provided
	var progressCh chan v1.Update
	var progressDone chan struct{}
	if opt.ProgressFn != nil {
		progressCh = make(chan v1.Update, 100)
		progressDone = make(chan struct{})
		digestStr := digest.String()
		go func() {
			defer close(progressDone)
			for update := range progressCh {
				opt.ProgressFn(ImagePushProgress{
					LayerDigest: digestStr,
					Complete:    update.Complete,
					Total:       update.Total,
				})
			}
		}()
	}

	writeErr := p.registry.WriteLayer(ctx, registry.WriteLayerOptions{
		Repo:       repo,
		Layer:      layer,
		ProgressCh: progressCh,
	})

	// Close the progress channel ourselves — WriteLayer sends to it but does not close it.
	if progressCh != nil {
		close(progressCh)
	}
	if progressDone != nil {
		<-progressDone
	}

	if writeErr != nil {
		return fmt.Errorf("push layer %s: %w", digest, writeErr)
	}

	return nil
}

// pushConfig pushes the image config blob to the registry.
// The config is typically small enough to be pushed as a single upload.
func (p *OCIImagePusher) pushConfig(ctx context.Context, repo string, img v1.Image) error {
	cfgBlob, err := img.RawConfigFile()
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}

	cfgName, err := img.ConfigName()
	if err != nil {
		return fmt.Errorf("get config digest: %w", err)
	}

	console.Debugf("Pushing config blob %s (%d bytes)", cfgName, len(cfgBlob))

	// Create a layer-like wrapper for the config blob to use WriteLayer
	configLayer := &configBlobLayer{
		data:   cfgBlob,
		digest: cfgName,
	}

	return p.registry.WriteLayer(ctx, registry.WriteLayerOptions{
		Repo:  repo,
		Layer: configLayer,
	})
}

// pushImageWithFallback pushes an image using the OCI chunked push path.
// Falls back to legacy Docker push if OCI push fails with a retryable/unknown error.
// Does NOT fall back on context cancellation or authentication errors.
func pushImageWithFallback(ctx context.Context, ociPusher *OCIImagePusher, dockerPusher *ImagePusher, artifact *ImageArtifact) error {
	if ociPusher != nil {
		err := ociPusher.Push(ctx, artifact.Reference)
		if err == nil {
			return nil
		}
		if !shouldFallbackToDocker(err) {
			return fmt.Errorf("OCI chunked push: %w", err)
		}
		console.Warnf("OCI chunked push failed, falling back to Docker push: %v", err)
	}
	return dockerPusher.PushArtifact(ctx, artifact)
}

// shouldFallbackToDocker returns true if the error is safe to fall back from.
// We do NOT fall back on context errors (cancellation/timeout) or auth failures.
func shouldFallbackToDocker(err error) bool {
	if err == nil {
		return false
	}
	// Never fall back on context cancellation or deadline
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return true
}

// getPushConcurrency returns the configured concurrency, checking env var override.
func getPushConcurrency() int {
	if v := os.Getenv(envPushConcurrency); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultPushConcurrency
}

// configBlobLayer wraps a config blob to satisfy the v1.Layer interface
// required by WriteLayerOptions.
type configBlobLayer struct {
	data   []byte
	digest v1.Hash
}

func (c *configBlobLayer) Digest() (v1.Hash, error) {
	return c.digest, nil
}

// DiffID returns the same hash as Digest. For uncompressed config blobs,
// the compressed and uncompressed representations are identical, so DiffID
// (hash of uncompressed content) equals Digest (hash of compressed content).
func (c *configBlobLayer) DiffID() (v1.Hash, error) {
	return c.digest, nil
}

func (c *configBlobLayer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(c.data)), nil
}

func (c *configBlobLayer) Uncompressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(c.data)), nil
}

func (c *configBlobLayer) Size() (int64, error) {
	return int64(len(c.data)), nil
}

func (c *configBlobLayer) MediaType() (types.MediaType, error) {
	return types.OCIConfigJSON, nil
}
