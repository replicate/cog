package model

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/replicate/cog/pkg/global"
)

// ParseOption configures how image references are parsed.
type ParseOption func(*parseOptions)

type parseOptions struct {
	nameOpts []name.Option
}

// Insecure allows parsing references to registries that use HTTP
// or have invalid/self-signed certificates.
// Use this for local development registries like localhost:5000.
func Insecure() ParseOption {
	return func(o *parseOptions) {
		o.nameOpts = append(o.nameOpts, name.Insecure)
	}
}

// WithDefaultRegistry sets the registry to use when the reference
// doesn't include one. By default, Docker Hub (index.docker.io) is used.
func WithDefaultRegistry(registry string) ParseOption {
	return func(o *parseOptions) {
		o.nameOpts = append(o.nameOpts, name.WithDefaultRegistry(registry))
	}
}

// WithDefaultTag sets the tag to use when the reference doesn't
// include one. By default, "latest" is used.
func WithDefaultTag(tag string) ParseOption {
	return func(o *parseOptions) {
		o.nameOpts = append(o.nameOpts, name.WithDefaultTag(tag))
	}
}

// ParsedRef wraps a validated and parsed image reference.
type ParsedRef struct {
	// Original is the input string before parsing.
	Original string

	// Ref is the underlying parsed reference from go-containerregistry.
	Ref name.Reference
}

// ParseRef validates and parses an image reference.
func ParseRef(ref string, opts ...ParseOption) (*ParsedRef, error) {
	var po parseOptions
	for _, opt := range opts {
		opt(&po)
	}

	parsed, err := name.ParseReference(ref, po.nameOpts...)
	if err != nil {
		return nil, fmt.Errorf("invalid image reference %q: %w", ref, err)
	}

	return &ParsedRef{
		Original: ref,
		Ref:      parsed,
	}, nil
}

// String returns the fully-qualified canonical reference string.
func (p *ParsedRef) String() string {
	return p.Ref.Name()
}

// Registry returns the registry host (e.g., "r8.im", "index.docker.io").
func (p *ParsedRef) Registry() string {
	return p.Ref.Context().RegistryStr()
}

// Repository returns the repository path (e.g., "user/model", "library/nginx").
func (p *ParsedRef) Repository() string {
	return p.Ref.Context().RepositoryStr()
}

// Tag returns the tag (e.g., "v1", "latest") or empty string if this is a digest reference.
func (p *ParsedRef) Tag() string {
	if t, ok := p.Ref.(name.Tag); ok {
		return t.TagStr()
	}
	return ""
}

// Digest returns the digest (e.g., "sha256:...") or empty string if this is a tag reference.
func (p *ParsedRef) Digest() string {
	if d, ok := p.Ref.(name.Digest); ok {
		return d.DigestStr()
	}
	return ""
}

// IsTag returns true if the reference includes a tag.
func (p *ParsedRef) IsTag() bool {
	_, ok := p.Ref.(name.Tag)
	return ok
}

// IsDigest returns true if the reference includes a digest.
func (p *ParsedRef) IsDigest() bool {
	_, ok := p.Ref.(name.Digest)
	return ok
}

// IsReplicate returns true if the registry is the Replicate registry (r8.im).
func (p *ParsedRef) IsReplicate() bool {
	return p.Registry() == global.ReplicateRegistryHost
}
