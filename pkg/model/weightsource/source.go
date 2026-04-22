package weightsource

import (
	"context"
	"fmt"
	"strings"
)

// Source is the provider for a weight-source scheme.
//
// Implementations translate a scheme-specific URI (file://, hf://, s3://,
// http:// …) into (a) a local directory ready for the packer, and (b) a
// version identity for the upstream state (Fingerprint).
//
// Source is stateless with respect to individual imports — the same
// instance may be used concurrently for many URIs of its scheme. Operations
// are expected to be context-cancellable.
type Source interface {
	// Fetch materializes the source as a local directory. For file://
	// sources this validates and returns the source path on disk. For
	// remote sources (future) it downloads to a caller-chosen or
	// implementation-managed temporary directory.
	//
	// Callers pass a project directory (the directory containing
	// cog.yaml) so that schemes using relative paths can resolve them
	// without reaching for process-global state.
	Fetch(ctx context.Context, uri, projectDir string) (localDir string, err error)

	// Fingerprint returns the source's version identity — a stable
	// identifier for the upstream state. For file://, this is
	// sha256:<setDigest> computed over the file set. For remote sources
	// it is a scheme-native identifier (commit:<sha>, etag:<value>, etc.).
	//
	// Fingerprint is required to be deterministic: the same URI pointing
	// at the same upstream state must always produce the same fingerprint
	// on repeated calls.
	Fingerprint(ctx context.Context, uri, projectDir string) (Fingerprint, error)
}

// For returns the Source implementation for the given URI's scheme.
//
// The scheme is the substring before the first "://". Bare paths (no
// scheme) are treated as file:// — this accepts both absolute ("/data") and
// relative ("./weights") forms as a convenience at the interface
// boundary.
//
// Unknown schemes return a clear error listing the currently supported
// schemes. This is the only place where scheme → implementation dispatch
// happens; adding hf:// or s3:// is a single case here plus the matching
// Source implementation (cog-9vfd).
func For(uri string) (Source, error) {
	scheme := schemeOf(uri)
	switch scheme {
	case "file", "":
		return FileSource{}, nil
	default:
		return nil, fmt.Errorf("unsupported weight source scheme %q (supported: file)", scheme)
	}
}

// schemeOf returns the scheme component of a URI, or "" for bare paths.
// It intentionally does not try to parse with net/url — hf://org/repo,
// s3://bucket/key, etc. are not RFC 3986-conformant URLs and net/url
// behaves inconsistently for them across schemes.
func schemeOf(uri string) string {
	scheme, _, ok := strings.Cut(uri, "://")
	if !ok {
		return ""
	}
	return scheme
}
