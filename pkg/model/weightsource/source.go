package weightsource

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Source is the provider for a weight-source scheme, bound at
// construction time to a specific URI.
//
// Implementations translate a scheme-specific URI (file://, hf://, s3://,
// http://, ...) into (a) an inventory of what the source contains, and
// (b) an on-demand byte stream for any one file in that inventory. The
// weights subsystem drives the import pipeline off these two capabilities
// — there is deliberately no "materialize the whole source to disk" step,
// so sources whose contents do not fit on local disk can still flow
// through the packer one file at a time.
//
// A Source instance is bound to one URI for its entire lifetime. Callers
// construct a Source via For(uri, projectDir). Methods are expected to be
// context-cancellable and safe to call concurrently for different paths.
type Source interface {
	// Inventory returns the file list and version identity for the
	// bound source. For file:// this walks and hashes (unavoidable for
	// a local directory). For future remote sources it is expected to
	// be cheap — HuggingFace Hub exposes per-file sha256 via its API,
	// OCI sources read them from the source manifest's config blob.
	Inventory(ctx context.Context) (Inventory, error)

	// Open returns a reader for a single file in the source, identified
	// by its inventory path (relative to the source root). Called on
	// demand during packing. The caller closes the returned reader.
	Open(ctx context.Context, path string) (io.ReadCloser, error)
}

// Inventory is the result of Source.Inventory: everything needed to plan
// an import without transferring payload bytes.
//
// Fingerprint is the source's version identity for the currently bound
// URI. Files is the list of content-addressed entries that make up the
// source; the packer consumes this list to produce tar layers.
type Inventory struct {
	Files       []InventoryFile
	Fingerprint Fingerprint
}

// InventoryFile is one entry in an Inventory: a file's relative path,
// size, and content digest. For file:// the digest is computed by
// walking and hashing; for remote sources it is read from a source-side
// index.
type InventoryFile struct {
	// Path is the file path relative to the source root, using forward
	// slashes regardless of the host OS.
	Path string
	// Size is the uncompressed file size in bytes.
	Size int64
	// Digest is the SHA-256 content digest with the "sha256:" prefix.
	Digest string
}

// DirhashParts implements Dirhashable so InventoryFile slices can be
// passed directly to DirHash.
func (f InventoryFile) DirhashParts() DirhashPart {
	return DirhashPart{Path: f.Path, Digest: f.Digest}
}

// sortInventoryFiles sorts files by path. Every Source implementation
// must return a sorted inventory; this helper enforces the convention.
func sortInventoryFiles(files []InventoryFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

// NormalizeURI returns the canonical form of a weight source URI.
//
// Each scheme has its own normalization rules:
//   - file:// and bare paths → canonical file:// form (see normalizeFileURI)
//   - hf:// and huggingface:// → canonical hf:// form (see normalizeHFURI)
//
// Empty strings and unsupported schemes return an error.
func NormalizeURI(uri string) (string, error) {
	if uri == "" {
		return "", fmt.Errorf("empty weight source uri")
	}

	scheme := schemeOf(uri)
	switch scheme {
	case "file", "":
		// Bare paths and file:// URIs. For bare paths the full URI is
		// the path; for file:// we strip the scheme prefix before
		// normalizing.
		path := uri
		if scheme == "file" {
			path = strings.TrimPrefix(uri, "file://")
		}
		return normalizeFileURI(path)
	case HFScheme, HFSchemeLong:
		return normalizeHFURI(uri)
	default:
		return "", fmt.Errorf("unsupported weight source scheme %q in URI %q", scheme, uri)
	}
}

// For returns the Source implementation for the given URI's scheme,
// bound to uri and projectDir.
//
// The scheme is the substring before the first "://". Bare paths (no
// scheme) are treated as file:// — this accepts both absolute ("/data")
// and relative ("./weights") forms as a convenience at the interface
// boundary.
//
// Unknown schemes return a clear error listing the currently supported
// schemes. This is the only place where scheme → implementation dispatch
// happens; adding s3:// or http:// is a single case here plus the
// matching Source implementation.
//
// For validates that the source exists and is usable. A file:// URI that
// points at a missing path or at a non-directory returns an error here,
// not at Inventory time.
func For(uri, projectDir string) (Source, error) {
	scheme := schemeOf(uri)
	switch scheme {
	case "file", "":
		return NewFileSource(uri, projectDir)
	case HFScheme, HFSchemeLong:
		return NewHFSource(uri)
	default:
		return nil, fmt.Errorf("unsupported weight source scheme %q (supported: file, hf, huggingface)", scheme)
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
