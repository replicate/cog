package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/getkin/kin-openapi/openapi3"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// LoadOption configures how Load resolves a model.
type LoadOption func(*loadOptions)

type loadOptions struct {
	localOnly   bool
	remoteOnly  bool
	preferLocal bool // default: true
	platform    *registry.Platform
}

func defaultLoadOptions() *loadOptions {
	return &loadOptions{preferLocal: true}
}

// LocalOnly loads only from the local docker daemon.
// Returns an error if the image is not found locally.
func LocalOnly() LoadOption {
	return func(o *loadOptions) {
		o.localOnly = true
		o.remoteOnly = false
		o.preferLocal = false
	}
}

// RemoteOnly loads only from the remote registry.
// Does not check the local docker daemon.
func RemoteOnly() LoadOption {
	return func(o *loadOptions) {
		o.remoteOnly = true
		o.localOnly = false
		o.preferLocal = false
	}
}

// PreferRemote tries remote registry first, falls back to local on not-found.
func PreferRemote() LoadOption {
	return func(o *loadOptions) {
		o.preferLocal = false
		o.localOnly = false
		o.remoteOnly = false
	}
}

// WithPlatform sets the platform for remote registry queries.
func WithPlatform(p *registry.Platform) LoadOption {
	return func(o *loadOptions) {
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

// Load resolves a parsed ref to a Model.
// By default (PreferLocal), tries local docker daemon first, then remote registry.
// Only falls back on "not found" errors; real errors (docker down, auth) are surfaced.
func (r *Resolver) Load(ctx context.Context, ref *ParsedRef, opts ...LoadOption) (*Model, error) {
	o := defaultLoadOptions()
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

// LoadByID loads a Model from the local docker daemon by image ID.
// This supports both full IDs (sha256:...) and short IDs (e.g., "9056219a5fb2").
// Use this when you have an image ID rather than a tagged reference.
func (r *Resolver) LoadByID(ctx context.Context, id string) (*Model, error) {
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

	// Parse config from labels
	var cfg *config.Config
	if configJSON := img.Config(); configJSON != "" {
		cfg = new(config.Config)
		if err := json.Unmarshal([]byte(configJSON), cfg); err != nil {
			return nil, fmt.Errorf("failed to parse cog config from labels: %w", err)
		}
	}

	// Parse schema from labels
	var schema *openapi3.T
	if schemaJSON := img.OpenAPISchema(); schemaJSON != "" {
		loader := openapi3.NewLoader()
		schema, _ = loader.LoadFromData([]byte(schemaJSON))
	}

	return &Model{
		Image:      img,
		Config:     cfg,
		Schema:     schema,
		CogVersion: img.CogVersion(),
	}, nil
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

	return r.modelFromImage(img, src.Config), nil
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
func (r *Resolver) modelFromImage(img *Image, cfg *config.Config) *Model {
	var schema *openapi3.T
	if schemaJSON := img.OpenAPISchema(); schemaJSON != "" {
		loader := openapi3.NewLoader()
		schema, _ = loader.LoadFromData([]byte(schemaJSON))
	}

	return &Model{
		Image:      img,
		Config:     cfg,
		Schema:     schema,
		CogVersion: img.CogVersion(),
	}
}

// modelFromInspect creates a Model from docker inspect response.
func (r *Resolver) modelFromInspect(ref *ParsedRef, resp *image.InspectResponse, source ImageSource) (*Model, error) {
	img := &Image{
		Reference: ref.String(),
		Digest:    resp.ID,
		Labels:    resp.Config.Labels,
		Source:    source,
	}

	// Parse config from labels
	var cfg *config.Config
	if configJSON := img.Config(); configJSON != "" {
		cfg = new(config.Config)
		if err := json.Unmarshal([]byte(configJSON), cfg); err != nil {
			return nil, fmt.Errorf("failed to parse cog config from labels: %w", err)
		}
	}

	// Parse schema from labels
	var schema *openapi3.T
	if schemaJSON := img.OpenAPISchema(); schemaJSON != "" {
		loader := openapi3.NewLoader()
		schema, _ = loader.LoadFromData([]byte(schemaJSON))
	}

	return &Model{
		Image:      img,
		Config:     cfg,
		Schema:     schema,
		CogVersion: img.CogVersion(),
	}, nil
}

// modelFromManifest creates a Model from registry manifest.
func (r *Resolver) modelFromManifest(ref *ParsedRef, manifest *registry.ManifestResult, source ImageSource) (*Model, error) {
	// Registry manifest has limited label info - primarily for existence checks.
	// Full model metadata requires pulling the image.
	// TODO: Add Digest field to ManifestResult and populate here.
	return &Model{
		Image: &Image{
			Reference: ref.String(),
			Digest:    "", // ManifestResult doesn't currently expose manifest digest
			Source:    source,
		},
	}, nil
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
