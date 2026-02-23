package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// ImagePusher pushes container images to a registry.
//
// It first attempts an OCI chunked push (export from Docker → OCI layout →
// push layers via registry client), then falls back to Docker's native push
// on any non-fatal error. This bypasses size limits on Docker's monolithic
// push path while maintaining backwards compatibility.
type ImagePusher struct {
	docker   command.Command
	registry registry.Client
}

// newImagePusher creates a new ImagePusher.
//
// If reg is nil, OCI chunked push is skipped and Docker push is used directly.
func newImagePusher(docker command.Command, reg registry.Client) *ImagePusher {
	return &ImagePusher{
		docker:   docker,
		registry: reg,
	}
}

// ImagePushOptions configures push behavior for ImagePusher.
type ImagePushOptions struct {
	// ProgressFn is an optional callback for reporting per-layer upload progress.
	ProgressFn func(PushProgress)
}

// Push pushes a container image to the registry by reference.
//
// Tries the OCI chunked push path first (if registry client is available),
// then falls back to Docker push on any non-fatal error.
func (p *ImagePusher) Push(ctx context.Context, imageRef string, opts ...ImagePushOptions) error {
	var opt ImagePushOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	if p.canOCIPush() {
		err := p.ociPush(ctx, imageRef, opt)
		if err == nil {
			return nil
		}
		if !shouldFallbackToDocker(err) {
			return fmt.Errorf("OCI chunked push: %w", err)
		}
		console.Warnf("OCI chunked push failed, falling back to Docker push: %v", err)
	}

	return p.docker.Push(ctx, imageRef)
}

// PushArtifact pushes a single image artifact by reference.
// This is a convenience method used by BundlePusher.
func (p *ImagePusher) PushArtifact(ctx context.Context, artifact *ImageArtifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact is nil")
	}
	if artifact.Reference == "" {
		return fmt.Errorf("image has no reference")
	}
	return p.Push(ctx, artifact.Reference)
}

// canOCIPush returns true if OCI chunked push is available.
func (p *ImagePusher) canOCIPush() bool {
	return p.registry != nil
}

// ociPush exports the image from Docker daemon to OCI layout, then pushes all layers,
// config, and manifest to the registry using chunked uploads.
func (p *ImagePusher) ociPush(ctx context.Context, imageRef string, opt ImagePushOptions) error {
	console.Debugf("Exporting image %s from Docker daemon...", imageRef)

	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return fmt.Errorf("parse image reference %q: %w", imageRef, err)
	}

	// Get the Docker tar stream directly from the docker command
	rc, err := p.docker.ImageSave(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("export image from daemon: %w", err)
	}
	defer rc.Close() //nolint:errcheck

	// Write the tar to a temp file so we can seek on it
	tmpTar, err := os.CreateTemp("", "cog-image-*.tar")
	if err != nil {
		return fmt.Errorf("create temp tar file: %w", err)
	}
	defer func() { _ = os.Remove(tmpTar.Name()) }()
	defer tmpTar.Close() //nolint:errcheck

	if _, err := io.Copy(tmpTar, rc); err != nil {
		return fmt.Errorf("write image tar: %w", err)
	}
	_ = rc.Close()

	// Load image from Docker tar using go-containerregistry
	tag, ok := ref.(name.Tag)
	if !ok {
		// If reference is a digest, use tag "latest" as a fallback
		tag = ref.Context().Tag("latest")
	}

	img, err := tarball.ImageFromPath(tmpTar.Name(), &tag)
	if err != nil {
		return fmt.Errorf("load image from tar: %w", err)
	}

	// Create a temp directory for the OCI layout
	dir, err := os.MkdirTemp("", "cog-oci-layout-*")
	if err != nil {
		return fmt.Errorf("create OCI layout directory: %w", err)
	}
	defer func() {
		console.Debugf("Cleaning up OCI layout directory: %s", dir)
		_ = os.RemoveAll(dir)
	}()

	console.Debugf("Writing OCI layout to %s", dir)
	lp, err := layout.Write(dir, empty.Index)
	if err != nil {
		return fmt.Errorf("initialize OCI layout: %w", err)
	}

	if err := lp.AppendImage(img); err != nil {
		return fmt.Errorf("write image to OCI layout: %w", err)
	}

	return p.pushImage(ctx, imageRef, img, opt)
}

// pushImage pushes a v1.Image (layers, config, manifest) to the registry.
func (p *ImagePusher) pushImage(ctx context.Context, imageRef string, img v1.Image, opt ImagePushOptions) error {
	repo := repoFromReference(imageRef)

	if err := p.pushLayers(ctx, repo, img, opt); err != nil {
		return fmt.Errorf("push layers: %w", err)
	}

	if err := p.pushConfig(ctx, repo, img); err != nil {
		return fmt.Errorf("push config: %w", err)
	}

	console.Debugf("Pushing image manifest for %s", imageRef)
	if err := p.registry.PushImage(ctx, imageRef, img); err != nil {
		return fmt.Errorf("push manifest: %w", err)
	}

	return nil
}

// pushLayers pushes all image layers concurrently using the registry client's
// WriteLayer method, which handles chunked uploads, retry, and progress reporting.
func (p *ImagePusher) pushLayers(ctx context.Context, repo string, img v1.Image, opt ImagePushOptions) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("get image layers: %w", err)
	}

	if len(layers) == 0 {
		return nil
	}

	concurrency := GetPushConcurrency()
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
func (p *ImagePusher) pushLayer(ctx context.Context, repo string, layer v1.Layer, opt ImagePushOptions) error {
	digest, err := layer.Digest()
	if err != nil {
		return fmt.Errorf("get layer digest: %w", err)
	}

	size, err := layer.Size()
	if err != nil {
		return fmt.Errorf("get layer size: %w", err)
	}

	console.Debugf("Pushing layer %s (%d bytes)", digest, size)

	var onProgress func(v1.Update)
	if opt.ProgressFn != nil {
		digestStr := digest.String()
		onProgress = func(update v1.Update) {
			opt.ProgressFn(PushProgress{
				LayerDigest: digestStr,
				Complete:    update.Complete,
				Total:       update.Total,
			})
		}
	}

	writeErr := writeLayerWithProgress(ctx, p.registry, registry.WriteLayerOptions{
		Repo:  repo,
		Layer: layer,
	}, onProgress)

	if writeErr != nil {
		return fmt.Errorf("push layer %s: %w", digest, writeErr)
	}

	return nil
}

// pushConfig pushes the image config blob to the registry.
func (p *ImagePusher) pushConfig(ctx context.Context, repo string, img v1.Image) error {
	cfgBlob, err := img.RawConfigFile()
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}

	cfgName, err := img.ConfigName()
	if err != nil {
		return fmt.Errorf("get config digest: %w", err)
	}

	console.Debugf("Pushing config blob %s (%d bytes)", cfgName, len(cfgBlob))

	configLayer := &configBlobLayer{
		data:   cfgBlob,
		digest: cfgName,
	}

	return p.registry.WriteLayer(ctx, registry.WriteLayerOptions{
		Repo:  repo,
		Layer: configLayer,
	})
}

// shouldFallbackToDocker returns true if the error is safe to fall back from.
// We do NOT fall back on context errors (cancellation/timeout).
func shouldFallbackToDocker(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
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
