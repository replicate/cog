package weightsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
// absolute on-disk path is resolved once at construction time so the
// Source methods do not re-resolve on every call.
type FileSource struct {
	// dir is the resolved absolute path to the source directory.
	dir string
	// fsys is an fs.FS rooted at dir. Open uses this instead of raw
	// os.Open to guarantee path-escape prevention at the FS boundary.
	fsys fs.FS
}

// NewFileSource constructs a FileSource bound to uri, resolving relative
// URIs against projectDir. It validates that the resolved path exists
// and is a directory.
func NewFileSource(uri, projectDir string) (*FileSource, error) {
	path, err := resolvePath(uri, projectDir)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("weight source not found: %s", uri)
		}
		return nil, fmt.Errorf("stat weight source %s: %w", uri, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("weight source %s is not a directory (file:// sources must be directories)", uri)
	}
	return &FileSource{dir: path, fsys: os.DirFS(path)}, nil
}

// sourceDir returns the resolved absolute path of the source directory.
// Exposed primarily for tests and diagnostics; the import pipeline should
// use Inventory + Open rather than reaching for the on-disk path.
func (s *FileSource) sourceDir() string { return s.dir }

// Inventory walks the source directory and returns per-file path / size /
// content digest plus the source fingerprint (sha256 of the sorted file
// set, spec §2.4).
//
// The .cog state directory is skipped. Non-regular entries (symlinks,
// devices, FIFOs, sockets) are rejected per spec §1.3 — silently
// dropping them would let a user ship a model missing files they
// expected. Resolve to regular files before importing.
func (s *FileSource) Inventory(ctx context.Context) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	return computeInventory(ctx, s.dir)
}

// Open returns a reader for a single file in the source, identified by
// its inventory path (relative to the source root, using forward
// slashes). The caller closes the returned reader.
func (s *FileSource) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := s.fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	rc, ok := f.(io.ReadCloser)
	if !ok {
		_ = f.Close()
		return nil, fmt.Errorf("open %s: filesystem does not support ReadCloser", path)
	}
	return rc, nil
}

// normalizeFileURI produces the canonical file:// URI for a path value
// that already has the file:// prefix stripped (or was never present).
func normalizeFileURI(path string) (string, error) {
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

// resolvePath turns a file:// URI or bare path into an absolute on-disk
// path, resolving relative paths against projectDir.
func resolvePath(uri, projectDir string) (string, error) {
	// Strip the file:// scheme if present; bare paths pass through.
	path := uri
	if rest, ok := strings.CutPrefix(uri, "file://"); ok {
		path = rest
	}
	normalized, err := normalizeFileURI(path)
	if err != nil {
		return "", err
	}
	// normalized is always "file://<path>" at this point.
	path = strings.TrimPrefix(normalized, "file://")
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
