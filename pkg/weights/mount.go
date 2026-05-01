package weights

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

// MountSpec describes one bind mount from host to container. Managed-
// weight mounts are always read-only; callers set ReadOnly on their
// container runtime's volume type unconditionally.
type MountSpec struct {
	Source string
	Target string
}

// Mounts is the handle returned from Prepare. It owns a per-invocation
// scratch directory under <projectDir>/.cog/mounts and MUST be
// released — either via Release or by the caller noticing the
// context was canceled.
type Mounts struct {
	Specs []MountSpec

	root string
}

// Release removes the per-invocation mount directory and every
// hardlink beneath it. The store's blobs are untouched.
// Release is idempotent and nil-safe.
func (m *Mounts) Release() error {
	if m == nil || m.root == "" {
		return nil
	}
	root := m.root
	m.root = ""
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove mount dir %s: %w", root, err)
	}
	return nil
}

// Prepare assembles per-invocation mount directories for every weight
// in the lockfile. Each weight gets its own directory populated by
// hardlinking blobs from the local store.
//
// If any file is missing from the store, Prepare returns an error
// directing the user at `cog weights pull`. v1 does NOT auto-pull.
//
// Hardlinks require the store and project dir to share a filesystem.
// On EXDEV, Prepare returns a clear error pointing at COG_CACHE_DIR;
// silent copy/symlink fallbacks would defeat the zero-duplication
// property.
//
// On any failure the partially-assembled invocation dir is removed
// before returning.
func (m *Manager) Prepare(ctx context.Context) (_ *Mounts, retErr error) {
	// A Manager configured for a weights-less model is a valid no-op:
	// Predictor can always call Prepare without checking whether
	// weights exist in cog.yaml.
	if m.lock == nil || len(m.lock.Weights) == 0 {
		return &Mounts{}, nil
	}

	if m.projectDir == "" {
		return nil, errors.New("prepare: Manager has no project dir")
	}

	invocationID, err := newInvocationID()
	if err != nil {
		return nil, fmt.Errorf("generate invocation id: %w", err)
	}
	root := filepath.Join(m.projectDir, global.CogBuildArtifactsFolder, "mounts", invocationID)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create mount root %s: %w", root, err)
	}

	mounts := &Mounts{root: root}

	defer func() {
		if retErr != nil {
			_ = mounts.Release()
		}
	}()

	for i := range m.lock.Weights {
		entry := &m.lock.Weights[i]
		weightDir, err := safeJoin(root, entry.Name)
		if err != nil {
			return nil, fmt.Errorf("weight %q: %w", entry.Name, err)
		}
		if err := m.assembleWeightDir(ctx, entry, weightDir); err != nil {
			return nil, err
		}
		mounts.Specs = append(mounts.Specs, MountSpec{
			Source: weightDir,
			Target: entry.Target,
		})
	}

	return mounts, nil
}

// safeJoin joins rel onto base and rejects the result if it escapes
// base. Lockfile entries are normally authored by `cog weights import`,
// but they're checked-in files a malicious or corrupt source could
// poison — filepath.Join cleans `..` components but doesn't prevent
// escape (`filepath.Join("/root", "../../etc")` returns `/etc`).
func safeJoin(base, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("empty path component")
	}
	cleanBase := filepath.Clean(base)
	joined := filepath.Clean(filepath.Join(cleanBase, rel))
	if !strings.HasPrefix(joined+string(filepath.Separator), cleanBase+string(filepath.Separator)) && joined != cleanBase {
		return "", fmt.Errorf("path %q escapes parent directory", rel)
	}
	return joined, nil
}

func (m *Manager) assembleWeightDir(ctx context.Context, entry *lockfile.WeightLockEntry, weightDir string) error {
	if err := os.MkdirAll(weightDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", weightDir, err)
	}
	for _, f := range entry.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		src, err := m.store.Path(ctx, f.Digest)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("weight %q is not fully cached locally (missing %s); run 'cog weights pull' first", entry.Name, f.Path)
			}
			return fmt.Errorf("locate %s (%s): %w", f.Path, f.Digest, err)
		}
		dst, err := safeJoin(weightDir, filepath.FromSlash(f.Path))
		if err != nil {
			return fmt.Errorf("weight %q file %q: %w", entry.Name, f.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create parent of %s: %w", dst, err)
		}
		if err := os.Link(src, dst); err != nil {
			return wrapLinkError(err, src, dst)
		}
	}
	return nil
}

// wrapLinkError decorates os.Link errors, explicitly diagnosing
// EXDEV — different filesystems for cache and project — because
// silent fallback (copy or symlink) would defeat the zero-duplication
// property or be unreliable inside bind-mounted containers.
func wrapLinkError(err error, src, dst string) error {
	if errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf(
			"hardlink %s -> %s failed: cache directory and project directory are on different filesystems. "+
				"Set COG_CACHE_DIR to a path on the same filesystem as your project, then re-run 'cog weights pull'. "+
				"underlying error: %w",
			src, dst, err,
		)
	}
	return fmt.Errorf("hardlink %s -> %s: %w", src, dst, err)
}

// newInvocationID returns 16 hex chars (2^64 distinct). Short enough
// for pleasant paths, wide enough that thousands of concurrent
// Predictors (e.g. across a CI matrix or a parallel testscript run)
// don't hit a birthday collision.
func newInvocationID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
