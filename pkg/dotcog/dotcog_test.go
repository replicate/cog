package dotcog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)
	defer d.Close()

	require.DirExists(t, d.Root())
	assert.True(t, filepath.IsAbs(d.Root()))
	assert.Equal(t, Name, filepath.Base(d.Root()))
}

func TestOpen_CreatesDirectory(t *testing.T) {
	parent := t.TempDir()
	sub := filepath.Join(parent, "nested", "project")
	d, err := Open(sub)
	require.NoError(t, err)
	defer d.Close()

	require.DirExists(t, filepath.Join(sub, Name))
}

func TestOpenTemp(t *testing.T) {
	d, err := OpenTemp()
	require.NoError(t, err)

	root := d.Root()
	require.DirExists(t, root)

	// Close removes the temp directory.
	require.NoError(t, d.Close())
	assert.NoDirExists(t, filepath.Dir(root))
}

func TestProjectDir(t *testing.T) {
	parent := t.TempDir()
	d, err := Open(parent)
	require.NoError(t, err)
	defer d.Close()

	// ProjectDir should be the parent of .cog/.
	absParent, _ := filepath.Abs(parent)
	assert.Equal(t, absParent, d.ProjectDir())
}

func TestPath(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)
	defer d.Close()

	p, err := d.Path("build")
	require.NoError(t, err)
	require.DirExists(t, p)
	assert.Equal(t, filepath.Join(d.Root(), "build"), p)

	// Nested paths work.
	p2, err := d.Path("cache/weights")
	require.NoError(t, err)
	require.DirExists(t, p2)
}

func TestTempPath_StartsClean(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)
	defer d.Close()

	// Create a file in a subdirectory.
	p, err := d.Path("build")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(p, "stale"), []byte("old"), 0o644))

	// TempPath wipes it.
	p2, err := d.TempPath("build")
	require.NoError(t, err)
	assert.Equal(t, p, p2)

	entries, err := os.ReadDir(p2)
	require.NoError(t, err)
	assert.Empty(t, entries, "TempPath should wipe existing contents")
}

func TestTempPath_CleanedOnClose(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)

	p, err := d.TempPath("build")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(p, "artifact"), []byte("data"), 0o644))

	require.NoError(t, d.Close())
	assert.NoDirExists(t, p)
}

func TestFilePath(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)
	defer d.Close()

	p, err := d.FilePath("cache/manifest.json")
	require.NoError(t, err)

	// Parent exists, but the file itself is not created.
	require.DirExists(t, filepath.Dir(p))
	assert.NoFileExists(t, p)
}

func TestClose_NilSafe(t *testing.T) {
	var d *Dir
	assert.NoError(t, d.Close())
}

func TestClose_Idempotent(t *testing.T) {
	d, err := OpenTemp()
	require.NoError(t, err)

	require.NoError(t, d.Close())
	require.NoError(t, d.Close()) // second call is a no-op
}

func TestClose_LIFO(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)

	var order []int
	d.onClose(func() error { order = append(order, 1); return nil })
	d.onClose(func() error { order = append(order, 2); return nil })
	d.onClose(func() error { order = append(order, 3); return nil })

	require.NoError(t, d.Close())
	assert.Equal(t, []int{3, 2, 1}, order)
}

func TestClose_JoinsErrors(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)

	d.onClose(func() error { return os.ErrPermission })
	d.onClose(func() error { return nil })
	d.onClose(func() error { return os.ErrNotExist })

	err = d.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.ErrorIs(t, err, os.ErrPermission)
}

func TestLock_AcquireAndRelease(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)
	defer d.Close()

	ctx := context.Background()
	release, err := d.Lock(ctx)
	require.NoError(t, err)

	// Lock file should exist.
	assert.FileExists(t, filepath.Join(d.Root(), lockFile))

	release()
}

func TestLock_BlocksUntilReleased(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)
	defer d.Close()

	ctx := context.Background()
	release1, err := d.Lock(ctx)
	require.NoError(t, err)

	// Second lock with a short timeout should fail.
	shortCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	_, err = d.Lock(shortCtx)
	require.Error(t, err, "second Lock should fail while first is held")

	// Release first, then second should succeed.
	release1()

	release2, err := d.Lock(ctx)
	require.NoError(t, err)
	release2()
}

func TestLock_CanceledContext(t *testing.T) {
	d, err := Open(t.TempDir())
	require.NoError(t, err)
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	_, err = d.Lock(ctx)
	require.Error(t, err)
}
