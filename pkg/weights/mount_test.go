package weights

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
)

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// primedManager returns a Manager whose store is pre-populated with
// every digest in bytesByDigest. The store is returned so tests can
// inspect it without reaching into unexported Manager fields.
func primedManager(t *testing.T, lock *lockfile.WeightsLock, bytesByDigest map[string][]byte) (*Manager, *store.FileStore) {
	t.Helper()
	fs, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	for digest, data := range bytesByDigest {
		require.NoError(t, fs.PutFile(context.Background(), digest, int64(len(data)), bytes.NewReader(data)))
	}
	mgr, err := NewManager(ManagerOptions{
		Store:      fs,
		Registry:   newStubRegistry(),
		Repo:       "example.com/me/model",
		Lock:       lock,
		ProjectDir: t.TempDir(),
	})
	require.NoError(t, err)
	return mgr, fs
}

func buildSimpleLock() (*lockfile.WeightsLock, map[string][]byte) {
	fileA := []byte("alpha content")
	fileB := []byte("bravo content")
	dA := sha256Of(fileA)
	dB := sha256Of(fileB)

	entry := lockfile.WeightLockEntry{
		Name:   "parakeet",
		Target: "/src/weights/parakeet",
		Files: []lockfile.WeightLockFile{
			{Path: "config.json", Size: int64(len(fileA)), Digest: dA, Layer: "sha256:deadbeef"},
			{Path: "model/weights.bin", Size: int64(len(fileB)), Digest: dB, Layer: "sha256:deadbeef"},
		},
	}
	return &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{entry}},
		map[string][]byte{dA: fileA, dB: fileB}
}

func TestPrepare_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lock, bytesByDigest := buildSimpleLock()
	mgr, _ := primedManager(t, lock, bytesByDigest)

	mounts, err := mgr.Prepare(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mounts.Release() })

	require.Len(t, mounts.Specs, 1)
	spec := mounts.Specs[0]
	assert.Equal(t, "/src/weights/parakeet", spec.Target)
	assert.Contains(t, spec.Source, filepath.Join(".cog", "mounts"))
	assert.True(t, filepath.IsAbs(spec.Source))

	for _, f := range lock.Weights[0].Files {
		onDisk := filepath.Join(spec.Source, filepath.FromSlash(f.Path))
		got, err := os.ReadFile(onDisk) //nolint:gosec // test-owned path
		require.NoError(t, err)
		require.Equal(t, bytesByDigest[f.Digest], got)
	}
}

func TestPrepare_HardlinksShareInodes(t *testing.T) {
	t.Parallel()
	// Avoiding byte duplication is the whole point of Prepare —
	// verify the mount and store point at the same inode.
	ctx := context.Background()
	lock, bytesByDigest := buildSimpleLock()
	mgr, fs := primedManager(t, lock, bytesByDigest)

	mounts, err := mgr.Prepare(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mounts.Release() })

	f := lock.Weights[0].Files[0]
	storePath, err := fs.Path(ctx, f.Digest)
	require.NoError(t, err)
	mountPath := filepath.Join(mounts.Specs[0].Source, filepath.FromSlash(f.Path))

	storeStat, err := os.Stat(storePath)
	require.NoError(t, err)
	mountStat, err := os.Stat(mountPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(storeStat, mountStat),
		"hard-linked mount file must share inode with store file")
}

func TestPrepare_MissingFile_ErrorMentionsPull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lock, bytesByDigest := buildSimpleLock()
	first := lock.Weights[0].Files[0].Digest
	mgr, _ := primedManager(t, lock, map[string][]byte{first: bytesByDigest[first]})

	_, err := mgr.Prepare(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cog weights pull")
	assert.Contains(t, err.Error(), "parakeet")
}

func TestPrepare_CleansUpOnFailure(t *testing.T) {
	t.Parallel()
	// A later weight being absent must not leak partial dirs for
	// earlier weights.
	ctx := context.Background()
	dataA := []byte("a")
	dataB := []byte("b")
	dA := sha256Of(dataA)
	dB := sha256Of(dataB)

	lock := &lockfile.WeightsLock{
		Version: 1,
		Weights: []lockfile.WeightLockEntry{
			{
				Name: "present", Target: "/w1",
				Files: []lockfile.WeightLockFile{{Path: "f", Size: 1, Digest: dA, Layer: "sha256:x"}},
			},
			{
				Name: "absent", Target: "/w2",
				Files: []lockfile.WeightLockFile{{Path: "f", Size: 1, Digest: dB, Layer: "sha256:y"}},
			},
		},
	}
	mgr, _ := primedManager(t, lock, map[string][]byte{dA: dataA}) // dB missing

	_, err := mgr.Prepare(ctx)
	require.Error(t, err)

	entries, err := os.ReadDir(filepath.Join(mgr.ProjectDir(), ".cog", "mounts"))
	if err == nil {
		assert.Empty(t, entries, "failed Prepare must not leave invocation dirs behind")
	} else {
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestMounts_Release_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lock, bytesByDigest := buildSimpleLock()
	mgr, _ := primedManager(t, lock, bytesByDigest)

	mounts, err := mgr.Prepare(ctx)
	require.NoError(t, err)

	require.NoError(t, mounts.Release())
	_, statErr := os.Stat(mounts.Specs[0].Source)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	require.NoError(t, mounts.Release())
}

func TestMounts_Release_NilSafe(t *testing.T) {
	t.Parallel()
	var m *Mounts
	require.NoError(t, m.Release())
}

func TestPrepare_RejectsPathTraversalInWeightName(t *testing.T) {
	t.Parallel()
	// A lockfile entry whose Name tries to escape the mount root must
	// be refused — the lockfile is normally authored by import, but
	// it's a checked-in file that could come from a hand-edit or an
	// untrusted fork.
	ctx := context.Background()
	data := []byte("x")
	d := sha256Of(data)
	lock := &lockfile.WeightsLock{
		Version: 1,
		Weights: []lockfile.WeightLockEntry{{
			Name:   "../escape",
			Target: "/w",
			Files:  []lockfile.WeightLockFile{{Path: "f", Size: 1, Digest: d, Layer: "sha256:x"}},
		}},
	}
	mgr, _ := primedManager(t, lock, map[string][]byte{d: data})

	_, err := mgr.Prepare(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escape")
}

func TestPrepare_RejectsPathTraversalInFilePath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := []byte("x")
	d := sha256Of(data)
	lock := &lockfile.WeightsLock{
		Version: 1,
		Weights: []lockfile.WeightLockEntry{{
			Name:   "m1",
			Target: "/w",
			Files: []lockfile.WeightLockFile{{
				Path: "../../etc/passwd", Size: 1, Digest: d, Layer: "sha256:x",
			}},
		}},
	}
	mgr, _ := primedManager(t, lock, map[string][]byte{d: data})

	_, err := mgr.Prepare(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escape")
}

func TestSafeJoin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		base    string
		rel     string
		wantErr bool
	}{
		{name: "simple", base: "/root", rel: "child", wantErr: false},
		{name: "nested", base: "/root", rel: "a/b/c", wantErr: false},
		{name: "parent escape", base: "/root", rel: "../outside", wantErr: true},
		{name: "double parent escape", base: "/root", rel: "../../etc", wantErr: true},
		// Absolute-looking paths are re-rooted under base by filepath.Join,
		// so they stay inside and are allowed.
		{name: "absolute path in rel gets re-rooted", base: "/root", rel: "/etc/passwd", wantErr: false},
		{name: "empty rel", base: "/root", rel: "", wantErr: true},
		{name: "dot stays in", base: "/root", rel: "./a", wantErr: false},
		{name: "parent then back in", base: "/root", rel: "a/../b", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeJoin(tt.base, tt.rel)
			if tt.wantErr {
				require.Error(t, err, "rel %q should be rejected", tt.rel)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWrapLinkError_EXDEV(t *testing.T) {
	t.Parallel()
	// Bare EXDEV.
	err := wrapLinkError(syscall.EXDEV, "/cache/blob", "/project/mount/file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COG_CACHE_DIR",
		"EXDEV error must point users at the COG_CACHE_DIR escape hatch")
	assert.Contains(t, err.Error(), "different filesystems")
	assert.Contains(t, err.Error(), "/cache/blob")
	assert.Contains(t, err.Error(), "/project/mount/file")
	assert.ErrorIs(t, err, syscall.EXDEV, "wrap must preserve EXDEV for errors.Is")
}

func TestWrapLinkError_EXDEV_ThroughLinkError(t *testing.T) {
	t.Parallel()
	// os.Link returns *os.LinkError wrapping syscall.EXDEV; errors.Is
	// unwraps through that chain, so the EXDEV branch must still fire.
	linkErr := &os.LinkError{Op: "link", Old: "/cache/blob", New: "/project/mount/file", Err: syscall.EXDEV}
	err := wrapLinkError(linkErr, "/cache/blob", "/project/mount/file")
	assert.Contains(t, err.Error(), "COG_CACHE_DIR")
}

func TestWrapLinkError_NonEXDEV(t *testing.T) {
	t.Parallel()
	// Other errors get a plain wrap — no COG_CACHE_DIR hint, since
	// that's EXDEV-specific advice.
	err := wrapLinkError(errors.New("disk full"), "/a", "/b")
	assert.Contains(t, err.Error(), "hardlink /a -> /b")
	assert.NotContains(t, err.Error(), "COG_CACHE_DIR")
}

func TestPrepare_NoProjectDirWithWeights(t *testing.T) {
	t.Parallel()
	// Weights-less Manager is a no-op regardless of projectDir; only
	// a lock with actual weights triggers the projectDir requirement.
	fs, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	mgr, err := NewManager(ManagerOptions{
		Store:    fs,
		Registry: newStubRegistry(),
		Repo:     "r",
		Lock: &lockfile.WeightsLock{
			Version: 1,
			Weights: []lockfile.WeightLockEntry{{Name: "w", Target: "/t"}},
		},
	})
	require.NoError(t, err)
	_, err = mgr.Prepare(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project dir")
}
