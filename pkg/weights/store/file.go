package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"
)

const (
	digestAlgorithm = "sha256"
	sha256HexLen    = 64
	filesDir        = "files"
)

// FileStore is a Store backed by a directory on the local filesystem.
// Files are stored content-addressed under <root>/files/sha256/<ab>/<hex>.
//
// FileStore is safe for concurrent use by multiple goroutines and
// processes: PutFile writes to a temporary file and atomically renames;
// reads are stateless.
type FileStore struct {
	root string
}

// NewFileStore returns a FileStore rooted at dir. The root and the
// files/sha256/ subtree are created if they don't exist.
func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, errors.New("file store: root directory must not be empty")
	}
	if err := os.MkdirAll(filepath.Join(dir, filesDir, digestAlgorithm), 0o755); err != nil {
		return nil, fmt.Errorf("file store: create root: %w", err)
	}
	return &FileStore{root: dir}, nil
}

// Root returns the on-disk root of the store.
func (s *FileStore) Root() string { return s.root }

// parseDigest splits and validates "sha256:<64-lowercase-hex>".
func parseDigest(digest string) (string, error) {
	algo, hexStr, ok := strings.Cut(digest, ":")
	if !ok {
		return "", fmt.Errorf("invalid digest %q: missing algorithm prefix", digest)
	}
	if algo != digestAlgorithm {
		return "", fmt.Errorf("invalid digest %q: only %s is supported", digest, digestAlgorithm)
	}
	if len(hexStr) != sha256HexLen {
		return "", fmt.Errorf("invalid digest %q: expected %d hex chars, got %d", digest, sha256HexLen, len(hexStr))
	}
	// hex.DecodeString tolerates uppercase; we require lowercase so
	// paths stay canonical.
	if strings.ToLower(hexStr) != hexStr {
		return "", fmt.Errorf("invalid digest %q: non-lowercase hex", digest)
	}
	if _, err := hex.DecodeString(hexStr); err != nil {
		return "", fmt.Errorf("invalid digest %q: %w", digest, err)
	}
	return hexStr, nil
}

func (s *FileStore) pathFor(hexStr string) string {
	return filepath.Join(s.root, filesDir, digestAlgorithm, hexStr[:2], hexStr)
}

func (s *FileStore) prefixDir(hexStr string) string {
	return filepath.Join(s.root, filesDir, digestAlgorithm, hexStr[:2])
}

// Exists reports whether a file with the given digest is in the store.
func (s *FileStore) Exists(_ context.Context, digest string) (bool, error) {
	hexStr, err := parseDigest(digest)
	if err != nil {
		return false, err
	}
	switch _, statErr := os.Stat(s.pathFor(hexStr)); {
	case statErr == nil:
		return true, nil
	case errors.Is(statErr, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("stat %s: %w", digest, statErr)
	}
}

// PutFile writes r to the store under expectedDigest, verifying the
// computed digest as it streams.
//
// Idempotency: if the digest is already present, r is drained to
// io.Discard and nil is returned. This matters because Pull streams a
// whole layer tar and may encounter files already stored from a
// previous pull — we need those to succeed without desyncing the tar.
func (s *FileStore) PutFile(ctx context.Context, expectedDigest string, _ int64, r io.Reader) error {
	hexStr, err := parseDigest(expectedDigest)
	if err != nil {
		return err
	}

	if ok, err := s.Exists(ctx, expectedDigest); err != nil {
		return err
	} else if ok {
		_, _ = io.Copy(io.Discard, r)
		return nil
	}

	prefix := s.prefixDir(hexStr)
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return fmt.Errorf("create prefix dir: %w", err)
	}

	tmp, err := os.CreateTemp(prefix, "put-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	reader := &ctxReader{ctx: ctx, r: io.TeeReader(r, hasher)}

	if _, err := io.Copy(tmp, reader); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", expectedDigest, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	gotHex := hex.EncodeToString(hasher.Sum(nil))
	if gotHex != hexStr {
		return fmt.Errorf("digest mismatch: expected sha256:%s, got sha256:%s", hexStr, gotHex)
	}

	final := s.pathFor(hexStr)
	// gosec G304/G703: tmpPath comes from os.CreateTemp inside prefix,
	// final is composed from the validated sha256 hex — both paths are
	// constrained to the store root by construction.
	if err := os.Rename(tmpPath, final); err != nil { //nolint:gosec // see comment above
		return fmt.Errorf("rename %s: %w", final, err)
	}
	tmpPath = ""
	return nil
}

// Path returns the on-disk path for the file at digest, or an error
// wrapping fs.ErrNotExist if the digest is not in the store.
func (s *FileStore) Path(ctx context.Context, digest string) (string, error) {
	hexStr, err := parseDigest(digest)
	if err != nil {
		return "", err
	}
	ok, err := s.Exists(ctx, digest)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("path %s: %w", digest, fs.ErrNotExist)
	}
	return s.pathFor(hexStr), nil
}

// List walks files/sha256/ and yields one FileInfo per entry. Stray
// files (stale temp files from interrupted writes, anything whose name
// isn't a 64-char hex digest) are skipped.
func (s *FileStore) List(ctx context.Context) iter.Seq2[FileInfo, error] {
	return func(yield func(FileInfo, error) bool) {
		root := filepath.Join(s.root, filesDir, digestAlgorithm)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && path == root {
					return nil
				}
				return err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if len(name) != sha256HexLen {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !yield(FileInfo{Digest: digestAlgorithm + ":" + name, Size: info.Size()}, nil) {
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil && !errors.Is(err, filepath.SkipAll) {
			yield(FileInfo{}, err)
		}
	}
}

// Delete removes the file at digest. Missing digests are not an error.
func (s *FileStore) Delete(_ context.Context, digest string) error {
	hexStr, err := parseDigest(digest)
	if err != nil {
		return err
	}
	switch err := os.Remove(s.pathFor(hexStr)); {
	case err == nil, errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("delete %s: %w", digest, err)
	}
}

// ctxReader makes an io.Reader cancelable at Read-boundary granularity.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

var _ Store = (*FileStore)(nil)
