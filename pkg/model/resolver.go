package model

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	return &options{} // Default: preferRemote (try registry first, fall back to local)
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

// PreferLocal tries local docker daemon first, falls back to remote on not-found.
func PreferLocal() Option {
	return func(o *options) {
		o.preferLocal = true
		o.localOnly = false
		o.remoteOnly = false
	}
}

// PreferRemote tries remote registry first, falls back to local on not-found.
// This is the default behavior.
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
	docker      command.Command
	registry    registry.Client
	factory     Factory
	imagePusher *ImagePusher
}

// NewResolver creates a Resolver with the default factory.
func NewResolver(docker command.Command, reg registry.Client) *Resolver {
	return &Resolver{
		docker:      docker,
		registry:    reg,
		factory:     DefaultFactory(docker, reg),
		imagePusher: NewImagePusher(docker, reg, docker.ImageSave),
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
	img := &ImageArtifact{
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
	model, err := r.loadLocal(ctx, ref)
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
	// TODO: Support platform option for multi-platform images
	_, err = r.docker.Pull(ctx, ref.String(), false)
	if err != nil {
		if isNotFoundError(err) {
			return nil, fmt.Errorf("image %s: %w", ref.Original, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to pull image %s: %w", ref.Original, err)
	}

	// Inspect the now-local image
	return r.loadLocal(ctx, ref)
}

// Build creates a Model by building from source.
func (r *Resolver) Build(ctx context.Context, src *Source, opts BuildOptions) (*Model, error) {
	if src == nil {
		return nil, fmt.Errorf("source is required for Build")
	}
	if src.Config == nil {
		return nil, fmt.Errorf("source.Config is required for Build")
	}
	if src.ProjectDir == "" {
		return nil, fmt.Errorf("source.ProjectDir is required for Build")
	}
	opts = opts.WithDefaults(src)

	// Build image artifact via ImageBuilder
	ib := NewImageBuilder(r.factory, r.docker, src, opts)
	imageSpec := NewImageSpec("model", opts.ImageName)
	imgResult, err := ib.Build(ctx, imageSpec)
	if err != nil {
		return nil, err
	}
	ia, ok := imgResult.(*ImageArtifact)
	if !ok {
		return nil, fmt.Errorf("unexpected artifact type from image builder: %T", imgResult)
	}

	m, err := r.modelFromImage(ia, src.Config)
	if err != nil {
		return nil, err
	}

	m.OCIIndex = opts.OCIIndex
	m.Artifacts = []Artifact{ia}

	// Build weight artifacts if OCI index mode is enabled
	lockPath := opts.WeightsLockPath
	if lockPath == "" {
		lockPath = filepath.Join(src.ProjectDir, WeightsLockFilename)
	}

	if opts.OCIIndex && len(src.Config.Weights) > 0 {
		wb := NewWeightBuilder(src, m.CogVersion, lockPath)
		for _, ws := range src.Config.Weights {
			spec := NewWeightSpec(ws.Name, ws.Source, ws.Target)
			artifact, buildErr := wb.Build(ctx, spec)
			if buildErr != nil {
				return nil, fmt.Errorf("build weight %q: %w", ws.Name, buildErr)
			}
			m.Artifacts = append(m.Artifacts, artifact)
		}

	}

	return m, nil
}

// Push pushes a Model to a container registry.
//
// Uses the OCI chunked push path (via ImagePusher) which bypasses Docker's
// monolithic push and supports layers of any size through chunked uploads.
// Falls back to legacy Docker push if OCI push is not available.
func (r *Resolver) Push(ctx context.Context, m *Model, opts PushOptions) error {
	if m.OCIIndex {
		pusher := NewBundlePusher(r.imagePusher, r.registry)
		return pusher.Push(ctx, m, opts)
	}

	imgArtifact := m.GetImageArtifact()
	if imgArtifact == nil {
		return fmt.Errorf("no image artifact in model")
	}

	return r.imagePusher.PushArtifact(ctx, imgArtifact)
}

// loadLocal loads a Model from the local docker daemon.
func (r *Resolver) loadLocal(ctx context.Context, ref *ParsedRef) (*Model, error) {
	resp, err := r.docker.Inspect(ctx, ref.String())
	if err != nil {
		if isNotFoundError(err) {
			return nil, fmt.Errorf("image %s not found locally: %w", ref.Original, err)
		}
		return nil, fmt.Errorf("failed to inspect local image %s: %w", ref.Original, err)
	}
	return r.modelFromInspect(ref, resp, ImageSourceLocal)
}

// loadRemote loads a Model from the remote registry.
func (r *Resolver) loadRemote(ctx context.Context, ref *ParsedRef, platform *registry.Platform) (*Model, error) {
	manifest, err := r.registry.Inspect(ctx, ref.String(), platform)
	if err != nil {
		if errors.Is(err, registry.NotFoundError) {
			return nil, fmt.Errorf("image %s not found in registry: %w", ref.Original, err)
		}
		return nil, fmt.Errorf("failed to inspect remote image %s: %w", ref.Original, err)
	}
	return r.modelFromManifest(ref, manifest, ImageSourceRemote)
}

// modelFromImage creates a Model from ImageArtifact with a known config (post-build).
// Uses the provided config rather than parsing from labels.
func (r *Resolver) modelFromImage(img *ImageArtifact, cfg *config.Config) (*Model, error) {
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
	img := &ImageArtifact{
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
	// Check if this is an OCI Index (v2 format)
	if isOCIIndex(manifest) {
		return r.modelFromIndex(ref, manifest, source)
	}

	// Standard image (v1 format)
	img := &ImageArtifact{
		Reference: ref.String(),
		Digest:    manifest.Config, // Config digest serves as image ID
		Labels:    manifest.Labels,
		Source:    source,
	}

	m, err := img.ToModel()
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", ref.Original, err)
	}
	return m, nil
}

// modelFromIndex creates a Model from an OCI Image Index.
// It extracts the image manifest and weights manifest from the index.
func (r *Resolver) modelFromIndex(ref *ParsedRef, manifest *registry.ManifestResult, source ImageSource) (*Model, error) {
	// Find the image manifest (skip unknown/unknown platform artifacts)
	imgManifest := findImageManifest(manifest.Manifests, nil)
	if imgManifest == nil {
		return nil, fmt.Errorf("no image manifest found in index %s", ref.Original)
	}

	// Create ImageArtifact from the image manifest
	img := &ImageArtifact{
		Reference: ref.String(),
		Digest:    imgManifest.Digest,
		Labels:    manifest.Labels, // Labels come from the index inspection
		Source:    source,
		Platform: &Platform{
			OS:           imgManifest.OS,
			Architecture: imgManifest.Architecture,
			Variant:      imgManifest.Variant,
		},
	}

	m, err := img.ToModel()
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", ref.Original, err)
	}

	m.Index = &Index{
		Digest:    manifest.Digest, // Content-addressable digest from registry
		Reference: ref.String(),
		MediaType: manifest.MediaType,
		Manifests: make([]IndexManifest, len(manifest.Manifests)),
	}

	// Populate index manifests
	for i, pm := range manifest.Manifests {
		im := IndexManifest{
			Digest:      pm.Digest,
			MediaType:   pm.MediaType,
			Size:        pm.Size,
			Annotations: pm.Annotations,
		}
		if pm.OS != "" {
			im.Platform = &Platform{
				OS:           pm.OS,
				Architecture: pm.Architecture,
				Variant:      pm.Variant,
			}
		}
		// Determine manifest type
		if pm.OS == PlatformUnknown && pm.Annotations != nil && pm.Annotations[AnnotationReferenceType] == AnnotationValueWeights {
			im.Type = ManifestTypeWeights
		} else {
			im.Type = ManifestTypeImage
		}
		m.Index.Manifests[i] = im
	}

	return m, nil
}

// isOCIIndex checks if the manifest result is an OCI Image Index.
func isOCIIndex(mr *registry.ManifestResult) bool {
	return mr.IsIndex()
}

// findWeightsManifest finds the weights manifest in an index.
// Returns nil if no weights manifest is found.
func findWeightsManifest(manifests []registry.PlatformManifest) *registry.PlatformManifest {
	for i := range manifests {
		m := &manifests[i]
		if m.Annotations != nil && m.Annotations[AnnotationReferenceType] == AnnotationValueWeights {
			return m
		}
	}
	return nil
}

// findImageManifest finds the model image manifest in an index.
// If platform is specified, matches on OS/Architecture.
// Skips artifacts (platform: unknown/unknown).
func findImageManifest(manifests []registry.PlatformManifest, platform *registry.Platform) *registry.PlatformManifest {
	for i := range manifests {
		m := &manifests[i]
		// Skip artifacts (unknown platform)
		if m.OS == PlatformUnknown {
			continue
		}
		// Match platform if specified
		if platform != nil {
			if m.OS != platform.OS || m.Architecture != platform.Architecture {
				continue
			}
		}
		return m
	}
	return nil
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
