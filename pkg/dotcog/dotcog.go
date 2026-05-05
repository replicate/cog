// Package dotcog manages the .cog/ project directory.
//
// The .cog/ directory is the local state directory for a Cog project.
// It stores build staging artifacts, weight caches, mount scratch space,
// and a project-wide advisory lock. This package provides a structured
// way to create, access, lock, and clean up the directory.
//
// Typical usage:
//
//	d, err := dotcog.Open(projectRoot)
//	if err != nil { ... }
//	defer d.Close()
//
//	buildDir := d.Path("build")  // .cog/build/, created on demand
//
// For operations that need exclusive access:
//
//	err := d.WithLock(ctx, func() error { ... })
package dotcog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// Name is the directory name inside a project root.
const Name = ".cog"

// lockFile is the flock target inside .cog/.
const lockFile = "cog.lock"

// pollInterval is the retry interval for blocking lock acquisition.
const pollInterval = 100 * time.Millisecond

// Dir is a handle to a project's .cog/ directory.
//
// It provides path accessors, an advisory lock, and cleanup registration.
// Always create via Open or OpenTemp; never construct directly.
type Dir struct {
	// root is the absolute path to the .cog/ directory itself.
	root string

	// temp indicates this Dir was created by OpenTemp and should be
	// removed entirely on Close.
	temp bool

	mu       sync.Mutex
	cleanups []func() error
}

// Open returns a Dir rooted at <projectDir>/.cog/, creating it if needed.
func Open(projectDir string) (*Dir, error) {
	root := filepath.Join(projectDir, Name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", root, err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Dir{root: abs}, nil
}

// OpenTemp creates a Dir in a new temporary directory. The directory and
// all contents are removed on Close.
func OpenTemp() (*Dir, error) {
	tmp, err := os.MkdirTemp("", "cog-build-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	root := filepath.Join(tmp, Name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, fmt.Errorf("create %s: %w", root, err)
	}
	return &Dir{root: root, temp: true}, nil
}

// Root returns the absolute path to the .cog/ directory.
func (d *Dir) Root() string {
	return d.root
}

// ProjectDir returns the absolute path to the parent project directory.
func (d *Dir) ProjectDir() string {
	return filepath.Dir(d.root)
}

// Path returns the absolute path to a subdirectory of .cog/, creating it
// if it doesn't exist. For example, d.Path("build") returns ".cog/build/".
func (d *Dir) Path(name string) (string, error) {
	p := filepath.Join(d.root, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", p, err)
	}
	return p, nil
}

// FilePath returns the absolute path to a file inside .cog/, ensuring
// the parent directory exists. Unlike Path, it does not create the leaf
// as a directory.
func (d *Dir) FilePath(name string) (string, error) {
	p := filepath.Join(d.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("create parent of %s: %w", p, err)
	}
	return p, nil
}

// OnClose registers a cleanup function to be called by Close. Functions
// are called in LIFO order. Errors are joined, not short-circuited.
func (d *Dir) OnClose(fn func() error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cleanups = append(d.cleanups, fn)
}

// Close runs all registered cleanup functions in reverse order. If the
// Dir was created by OpenTemp, the entire temp tree is removed afterward.
// Close is nil-safe: calling Close on a nil *Dir is a no-op.
func (d *Dir) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	cleanups := d.cleanups
	d.cleanups = nil
	d.mu.Unlock()

	var errs []error
	for i := len(cleanups) - 1; i >= 0; i-- {
		if err := cleanups[i](); err != nil {
			errs = append(errs, err)
		}
	}
	if d.temp {
		// root is .cog/ inside the temp dir; remove the parent.
		if err := os.RemoveAll(filepath.Dir(d.root)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Lock acquires an exclusive project-wide advisory lock, blocking until
// the lock is available or ctx is canceled.
//
// Any mutating operation on the .cog/ directory (builds, weights imports)
// should hold this lock so concurrent cog invocations against the same
// project don't race.
//
// Returns a release function that must be called (typically via defer) to
// release the lock. The release function is safe to call even if Lock
// returned an error (it is a no-op in that case).
func (d *Dir) Lock(ctx context.Context) (release func(), err error) {
	noop := func() {}
	lockPath := filepath.Join(d.root, lockFile)
	fl := flock.New(lockPath)
	locked, err := fl.TryLockContext(ctx, pollInterval)
	if err != nil {
		return noop, fmt.Errorf("acquire project lock: %w", err)
	}
	if !locked {
		return noop, ctx.Err()
	}
	return func() {
		_ = fl.Unlock()
	}, nil
}

// Remove deletes the entire .cog/ directory.
func (d *Dir) Remove() error {
	return os.RemoveAll(d.root)
}
