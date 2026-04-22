package weightsource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileScheme is the URI scheme for local filesystem sources.
const FileScheme = "file"

// FileSource is the Source implementation for file:// URIs and bare paths.
//
// URIs take one of these forms:
//
//	file:///abs/path      — absolute path
//	file://./rel/path     — canonical relative path (explicit ./)
//	/abs/path             — bare absolute path (normalized to file://)
//	./rel/path            — bare relative path (normalized to file://)
//	rel/path              — bare relative path, no ./ prefix (normalized)
//
// The lockfile stores only the normalized form (see NormalizeURI); the
// absolute on-disk path is resolved on demand in Fetch so lockfiles remain
// portable across machines and check-outs.
type FileSource struct{}

// Fetch validates the URI, resolves it against projectDir if relative, and
// returns the absolute directory path. It does not copy or materialize
// anything — file:// sources already live on disk.
func (FileSource) Fetch(ctx context.Context, uri, projectDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	path, err := resolvePath(uri, projectDir)
	if err != nil {
		return "", err
	}

	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("weight source not found: %s", uri)
		}
		return "", fmt.Errorf("stat weight source %s: %w", uri, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("weight source %s is not a directory (file:// sources must be directories)", uri)
	}
	return path, nil
}

// Fingerprint walks the source directory and computes sha256:<setDigest>
// over the file set.
//
// Callers that already compute the set digest as part of packing (the
// import path) may skip this call and construct the fingerprint directly
// — for file:// sources the two are by definition identical. Fingerprint
// exists to satisfy the Source interface contract for callers that only
// want the version identity (e.g. a future `cog weights check` that
// compares the source against the recorded fingerprint without repacking).
func (FileSource) Fingerprint(ctx context.Context, uri, projectDir string) (Fingerprint, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := resolvePath(uri, projectDir)
	if err != nil {
		return "", err
	}
	setDigest, err := computeDirSetDigest(ctx, path)
	if err != nil {
		return "", fmt.Errorf("fingerprint %s: %w", uri, err)
	}
	return Fingerprint(setDigest), nil
}

// NormalizeURI returns the canonical file:// form of a URI.
//
// Rules:
//   - bare absolute paths become file://<path>
//   - bare relative paths become file://./<path>, with filepath.Clean
//     applied first so "weights/." normalizes to "file://./weights"
//   - file:// URIs are returned unchanged apart from path cleaning
//   - Empty strings and malformed URIs return an error
//
// The canonical relative form always uses the explicit ./ prefix. This
// makes "file:// + relative path" visually distinct from an accidental
// "file:/weights" (a scheme with an absolute path missing one slash).
func NormalizeURI(uri string) (string, error) {
	if uri == "" {
		return "", fmt.Errorf("empty weight source uri")
	}

	scheme, rest, hasScheme := strings.Cut(uri, "://")
	if !hasScheme {
		// Bare path — treat as file://.
		return normalizeFilePath(uri)
	}

	switch scheme {
	case FileScheme:
		return normalizeFilePath(rest)
	default:
		return "", fmt.Errorf("cannot normalize %q as file:// URI: scheme is %q", uri, scheme)
	}
}

// normalizeFilePath produces the canonical file:// URI for a path value
// that already has the file:// prefix stripped (or was never present).
func normalizeFilePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty weight source path")
	}

	// On some forms (file:///abs/path) the caller has already stripped
	// "file://", leaving "/abs/path". Bare "/abs/path" is the same
	// string, so we treat them uniformly.
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return "file://" + cleaned, nil
	}

	// filepath.Clean drops a leading "./"; re-add it so the relative
	// form is visually unambiguous. "." collapses to itself — callers
	// who point at the project dir ("") should not reach here; that's
	// rejected upstream.
	if cleaned == "." {
		return "", fmt.Errorf("weight source cannot be the project directory itself")
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("weight source %q escapes the project directory", path)
	}
	return "file://./" + cleaned, nil
}

// resolvePath turns a URI into an absolute on-disk path, resolving
// relative paths against projectDir. The URI must already be in canonical
// form (see NormalizeURI), but this function also accepts bare paths as a
// convenience.
func resolvePath(uri, projectDir string) (string, error) {
	normalized, err := NormalizeURI(uri)
	if err != nil {
		return "", err
	}
	// normalized is always "file://<path>" at this point.
	path := strings.TrimPrefix(normalized, "file://")
	if filepath.IsAbs(path) {
		return path, nil
	}
	// Relative: resolve against the project directory. The canonical
	// form has a "./" prefix — trim it so filepath.Join doesn't double up.
	path = strings.TrimPrefix(path, "./")
	if projectDir == "" {
		return "", fmt.Errorf("relative weight source %q requires a project directory", uri)
	}
	return filepath.Join(projectDir, path), nil
}
