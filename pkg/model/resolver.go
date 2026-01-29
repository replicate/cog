package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/image"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// Option configures how Resolver methods behave.
type Option func(*options)

type options struct {
	localOnly   bool
	remoteOnly  bool
	preferLocal bool // default: true
	platform    *registry.Platform
}

func defaultOptions() *options {
	return &options{preferLocal: true}
}

// LocalOnly loads only from the local docker daemon.
// Returns an error if the image is not found locally.
func LocalOnly() Option {
	return func(o *options) {
		o.localOnly = true
		o.remoteOnly = false
		o.preferLocal = false
	}
}

// RemoteOnly loads only from the remote registry.
// Does not check the local docker daemon.
func RemoteOnly() Option {
	return func(o *options) {
		o.remoteOnly = true
		o.localOnly = false
		o.preferLocal = false
	}
}

// PreferRemote tries remote registry first, falls back to local on not-found.
func PreferRemote() Option {
	return func(o *options) {
		o.preferLocal = false
		o.localOnly = false
		o.remoteOnly = false
	}
}

// WithPlatform sets the platform for remote registry queries.
func WithPlatform(p *registry.Platform) Option {
	return func(o *options) {
		o.platform = p
	}
}

// Resolver orchestrates building and loading Models.
type Resolver struct {
	docker   command.Command
	registry registry.Client
	factory  Factory
}

// NewResolver creates a Resolver with the default factory.
func NewResolver(docker command.Command, reg registry.Client) *Resolver {
	return &Resolver{
		docker:   docker,
		registry: reg,
		factory:  DefaultFactory(docker, reg),
	}
}

// WithFactory sets a custom factory and returns the Resolver for chaining.
func (r *Resolver) WithFactory(f Factory) *Resolver {
	r.factory = f
	return r
}

// Inspect returns Model metadata for a parsed ref without pulling.
// By default (PreferLocal), tries local docker daemon first, then remote registry.
// Only falls back on "not found" errors; real errors (docker down, auth) are surfaced.
// Returns ErrNotCogModel if the image is not a valid Cog model.
func (r *Resolver) Inspect(ctx context.Context, ref *ParsedRef, opts ...Option) (*Model, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	switch {
	case o.localOnly:
		return r.loadLocal(ctx, ref)
	case o.remoteOnly:
		return r.loadRemote(ctx, ref, o.platform)
	case o.preferLocal:
		model, localErr := r.loadLocal(ctx, ref)
		if localErr == nil {
			return model, nil
		}
		// Check the underlying error before the wrapper adds "not found" text
		if !isNotFoundError(errors.Unwrap(localErr)) {
			return nil, localErr // Real error, don't mask it
		}
		return r.loadRemote(ctx, ref, o.platform)
	default:
		// PreferRemote
		model, remoteErr := r.loadRemote(ctx, ref, o.platform)
		if remoteErr == nil {
			return model, nil
		}
		// Check the underlying error before the wrapper adds "not found" text
		if !isNotFoundError(errors.Unwrap(remoteErr)) {
			return nil, remoteErr
		}
		return r.loadLocal(ctx, ref)
	}
}

// InspectByID returns Model metadata from the local docker daemon by image ID.
// This supports both full IDs (sha256:...) and short IDs (e.g., "9056219a5fb2").
// Use this when you have an image ID rather than a tagged reference.
// Returns ErrNotCogModel if the image is not a valid Cog model.
func (r *Resolver) InspectByID(ctx context.Context, id string) (*Model, error) {
	resp, err := r.docker.Inspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to load image by ID %s: %w", id, err)
	}

	// Use the canonical ID from the response as the reference
	img := &Image{
		Reference: resp.ID,
		Digest:    resp.ID,
		Labels:    resp.Config.Labels,
		Source:    ImageSourceLocal,
	}

	model, err := img.ToModel()
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", id, err)
	}
	return model, nil
}

// Pull ensures a Model is locally available for running.
// It first checks if the image exists locally. If not, it pulls from the registry.
// Returns ErrNotCogModel if the image is not a valid Cog model.
// Returns ErrNotFound if the image cannot be found locally or remotely.
func (r *Resolver) Pull(ctx context.Context, ref *ParsedRef, opts ...Option) (*Model, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	// First, try to inspect locally
	model, err := r.inspectLocal(ctx, ref)
	if err == nil {
		return model, nil
	}

	// If local-only mode, don't try to pull
	if o.localOnly {
		return nil, fmt.Errorf("image %s: %w", ref.Original, ErrNotFound)
	}

	// If local image exists but isn't a Cog model, don't try to pull
	// (pulling won't change the existing image)
	if errors.Is(err, ErrNotCogModel) {
		return nil, err
	}

	// Check if it's a "not found" error (safe to try pull)
	if !isNotFoundError(errors.Unwrap(err)) {
		// Real error (connection refused, etc.) - don't mask it
		return nil, err
	}

	// Pull the image
	_, err = r.docker.Pull(ctx, ref.String(), false)
	if err != nil {
		if isNotFoundError(err) {
			return nil, fmt.Errorf("image %s: %w", ref.Original, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to pull image %s: %w", ref.Original, err)
	}

	// Inspect the now-local image
	return r.inspectLocal(ctx, ref)
}

// inspectLocal loads a Model from the local docker daemon only.
func (r *Resolver) inspectLocal(ctx context.Context, ref *ParsedRef) (*Model, error) {
	return r.loadLocal(ctx, ref)
}

// Build creates a Model by building from source.
func (r *Resolver) Build(ctx context.Context, src *Source, opts BuildOptions) (*Model, error) {
	opts = opts.WithDefaults(src)

	img, err := r.factory.Build(ctx, src, opts)
	if err != nil {
		return nil, err
	}

	// Inspect the built image to get labels.
	// Prefer using the image digest (ID) for stable lookups,
	// falling back to tag if Digest is empty (for backwards compatibility
	// with custom Factory implementations that don't populate Digest).
	inspectRef := img.Digest
	if inspectRef == "" {
		inspectRef = img.Reference
	}

	resp, err := r.docker.Inspect(ctx, inspectRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect built image: %w", err)
	}

	img.Labels = resp.Config.Labels
	// Use the canonical ID from the response
	img.Digest = resp.ID

	return r.modelFromImage(img, src.Config)
}

// BuildBase creates a base image for dev mode (without /src copied).
// The source directory is expected to be mounted as a volume at runtime.
// Returns a Model with the built image info and the source config.
func (r *Resolver) BuildBase(ctx context.Context, src *Source, opts BuildBaseOptions) (*Model, error) {
	opts = opts.WithDefaults()

	img, err := r.factory.BuildBase(ctx, src, opts)
	if err != nil {
		return nil, err
	}

	// For base builds, we don't have labels yet (they're added in full builds).
	// Return the model with the source config.
	return &Model{
		Image:  img,
		Config: src.Config,
	}, nil
}

// loadLocal loads a Model from the local docker daemon.
func (r *Resolver) loadLocal(ctx context.Context, ref *ParsedRef) (*Model, error) {
	resp, err := r.docker.Inspect(ctx, ref.String())
	if err != nil {
		return nil, fmt.Errorf("image %s not found locally: %w", ref.Original, err)
	}
	return r.modelFromInspect(ref, resp, ImageSourceLocal)
}

// loadRemote loads a Model from the remote registry.
func (r *Resolver) loadRemote(ctx context.Context, ref *ParsedRef, platform *registry.Platform) (*Model, error) {
	manifest, err := r.registry.Inspect(ctx, ref.String(), platform)
	if err != nil {
		return nil, fmt.Errorf("image %s not found in registry: %w", ref.Original, err)
	}
	return r.modelFromManifest(ref, manifest, ImageSourceRemote)
}

// modelFromImage creates a Model from Image with a known config (post-build).
// Uses the provided config rather than parsing from labels.
func (r *Resolver) modelFromImage(img *Image, cfg *config.Config) (*Model, error) {
	schema, err := img.ParsedOpenAPISchema()
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema from image labels: %w", err)
	}

	return &Model{
		Image:      img,
		Config:     cfg,
		Schema:     schema,
		CogVersion: img.CogVersion(),
	}, nil
}

// modelFromInspect creates a Model from docker inspect response.
// Returns ErrNotCogModel if the image is not a valid Cog model.
func (r *Resolver) modelFromInspect(ref *ParsedRef, resp *image.InspectResponse, source ImageSource) (*Model, error) {
	img := &Image{
		Reference: ref.String(),
		Digest:    resp.ID,
		Labels:    resp.Config.Labels,
		Source:    source,
	}

	model, err := img.ToModel()
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", ref.Original, err)
	}
	return model, nil
}

// modelFromManifest creates a Model from registry manifest.
// Returns ErrNotCogModel if the image is not a valid Cog model.
func (r *Resolver) modelFromManifest(ref *ParsedRef, manifest *registry.ManifestResult, source ImageSource) (*Model, error) {
	img := &Image{
		Reference: ref.String(),
		Digest:    manifest.Config, // Config digest serves as image ID
		Labels:    manifest.Labels,
		Source:    source,
	}

	model, err := img.ToModel()
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", ref.Original, err)
	}
	return model, nil
}

// isNotFoundError checks if an error indicates "not found" vs a real error.
// Only "not found" errors should trigger fallback to alternative source.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// Don't treat context errors as "not found"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for registry NotFoundError
	if errors.Is(err, registry.NotFoundError) {
		return true
	}

	// Check for common not-found patterns in error strings
	errStr := err.Error()
	return strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "No such image") ||
		strings.Contains(errStr, "manifest unknown") ||
		strings.Contains(errStr, "NAME_UNKNOWN")
}
