package model

import "context"

// Ref represents something that can be resolved to a Model.
// This interface enables declarative model resolution - callers specify
// "what they have" (a tag, local image, or source to build) and the
// Resolver figures out how to produce a Model.
type Ref interface {
	// resolve is unexported to keep the interface internal.
	// External code uses Resolver.Resolve() instead of calling this directly.
	resolve(ctx context.Context, r *Resolver) (*Model, error)
}

// Resolve resolves any Ref to a Model.
// This is the main entry point for declarative model resolution.
func (r *Resolver) Resolve(ctx context.Context, ref Ref) (*Model, error) {
	return ref.resolve(ctx, r)
}

// =============================================================================
// TagRef - resolves an image by tag/digest, trying local then remote
// =============================================================================

// TagRef resolves an image by tag or digest reference.
// It uses the default Load behavior: try remote registry first,
// then fall back to local docker daemon if not found remotely.
type TagRef struct {
	Parsed *ParsedRef
}

// FromTag parses and validates a tag reference.
// Returns an error immediately if the reference is invalid.
func FromTag(ref string) (*TagRef, error) {
	parsed, err := ParseRef(ref)
	if err != nil {
		return nil, err
	}
	return &TagRef{Parsed: parsed}, nil
}

func (t *TagRef) resolve(ctx context.Context, r *Resolver) (*Model, error) {
	// Use default Inspect behavior (PreferRemote)
	return r.Inspect(ctx, t.Parsed)
}

// =============================================================================
// LocalRef - explicitly loads from docker daemon only
// =============================================================================

// LocalRef resolves an image from the local docker daemon only.
// It will not fall back to remote registry if the image is not found locally.
type LocalRef struct {
	Parsed *ParsedRef
}

// FromLocal parses and validates a reference for local resolution.
// Returns an error immediately if the reference is invalid.
func FromLocal(ref string) (*LocalRef, error) {
	parsed, err := ParseRef(ref)
	if err != nil {
		return nil, err
	}
	return &LocalRef{Parsed: parsed}, nil
}

func (l *LocalRef) resolve(ctx context.Context, r *Resolver) (*Model, error) {
	return r.Inspect(ctx, l.Parsed, LocalOnly())
}

// =============================================================================
// RemoteRef - explicitly loads from registry only
// =============================================================================

// RemoteRef resolves an image from a remote registry only.
// It will not check the local docker daemon.
type RemoteRef struct {
	Parsed *ParsedRef
}

// FromRemote parses and validates a reference for remote resolution.
// Returns an error immediately if the reference is invalid.
func FromRemote(ref string) (*RemoteRef, error) {
	parsed, err := ParseRef(ref)
	if err != nil {
		return nil, err
	}
	return &RemoteRef{Parsed: parsed}, nil
}

func (rr *RemoteRef) resolve(ctx context.Context, r *Resolver) (*Model, error) {
	return r.Inspect(ctx, rr.Parsed, RemoteOnly())
}

// =============================================================================
// BuildRef - creates a Model by building from source
// =============================================================================

// BuildRef resolves to a Model by building from source.
// This wraps a Source and BuildOptions for deferred building.
type BuildRef struct {
	Source  *Source
	Options BuildOptions
}

// FromBuild creates a BuildRef from source and options.
// Unlike the other From* functions, this does not validate eagerly -
// validation happens at build time.
func FromBuild(src *Source, opts BuildOptions) *BuildRef {
	return &BuildRef{Source: src, Options: opts}
}

func (b *BuildRef) resolve(ctx context.Context, r *Resolver) (*Model, error) {
	return r.Build(ctx, b.Source, b.Options)
}
