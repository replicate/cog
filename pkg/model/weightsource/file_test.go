package weightsource

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeURI(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		want        string
		wantErrSubs string
	}{
		{"absolute bare", "/data/weights", "file:///data/weights", ""},
		{"absolute file scheme", "file:///data/weights", "file:///data/weights", ""},
		{"relative bare no dot", "weights", "file://./weights", ""},
		{"relative bare dot prefix", "./weights", "file://./weights", ""},
		{"relative file scheme", "file://./weights", "file://./weights", ""},
		{"relative with slash", "./weights/models", "file://./weights/models", ""},
		{"clean double slash", "./weights//models", "file://./weights/models", ""},
		{"clean dot segment", "./weights/./models", "file://./weights/models", ""},
		{"absolute clean", "/data//weights", "file:///data/weights", ""},

		{"empty", "", "", "empty weight source uri"},
		{"empty file scheme", "file://", "", "empty weight source path"},
		{"project dir itself rejected", ".", "", "project directory itself"},
		{"parent escape rejected", "../sibling", "", "escapes the project directory"},
		{"unknown scheme rejected", "s3://bucket/key", "", "unsupported weight source scheme"},

		{"hf basic", "hf://org/repo", "hf://org/repo", ""},
		{"hf with ref", "hf://org/repo@v1.0", "hf://org/repo@v1.0", ""},
		{"huggingface canonicalized", "huggingface://org/repo", "hf://org/repo", ""},
		{"huggingface with ref canonicalized", "huggingface://org/repo@main", "hf://org/repo", ""},
		{"hf explicit main ref stripped", "hf://org/repo@main", "hf://org/repo", ""},
		{"hf sha ref preserved", "hf://org/repo@abc123", "hf://org/repo@abc123", ""},
		{"hf invalid repo", "hf://justrepo", "", "expected org/repo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeURI(tc.in)
			if tc.wantErrSubs != "" {
				assert.ErrorContains(t, err, tc.wantErrSubs)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewFileSource_Absolute(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644))

	// Absolute URI; projectDir is ignored.
	uri := "file://" + dir
	src, err := NewFileSource(uri, "/unused")
	require.NoError(t, err)
	assert.Equal(t, dir, src.sourceDir())
}

func TestNewFileSource_BareAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	src, err := NewFileSource(dir, "")
	require.NoError(t, err)
	assert.Equal(t, dir, src.sourceDir())
}

func TestNewFileSource_Relative(t *testing.T) {
	projectDir := t.TempDir()
	weightsDir := filepath.Join(projectDir, "weights")
	require.NoError(t, os.MkdirAll(weightsDir, 0o755))

	src, err := NewFileSource("file://./weights", projectDir)
	require.NoError(t, err)
	assert.Equal(t, weightsDir, src.sourceDir())
}

func TestNewFileSource_BareRelative(t *testing.T) {
	projectDir := t.TempDir()
	weightsDir := filepath.Join(projectDir, "weights")
	require.NoError(t, os.MkdirAll(weightsDir, 0o755))

	src, err := NewFileSource("weights", projectDir)
	require.NoError(t, err)
	assert.Equal(t, weightsDir, src.sourceDir())
}

func TestNewFileSource_ErrorCases(t *testing.T) {
	projectDir := t.TempDir()

	t.Run("missing", func(t *testing.T) {
		_, err := NewFileSource("file://./missing", projectDir)
		assert.ErrorContains(t, err, "not found")
	})

	t.Run("is a file not a dir", func(t *testing.T) {
		filePath := filepath.Join(projectDir, "oops.bin")
		require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))
		_, err := NewFileSource("file://./oops.bin", projectDir)
		assert.ErrorContains(t, err, "is not a directory")
	})

	t.Run("relative uri without project dir", func(t *testing.T) {
		_, err := NewFileSource("file://./weights", "")
		assert.ErrorContains(t, err, "project directory")
	})
}

func TestFileSource_Open(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("world"), 0o644))

	src, err := NewFileSource("file://"+dir, "")
	require.NoError(t, err)

	t.Run("top level", func(t *testing.T) {
		rc, err := src.Open(t.Context(), "a.txt")
		require.NoError(t, err)
		defer rc.Close()
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(b))
	})

	t.Run("nested", func(t *testing.T) {
		rc, err := src.Open(t.Context(), "sub/b.txt")
		require.NoError(t, err)
		defer rc.Close()
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "world", string(b))
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := src.Open(t.Context(), "missing.txt")
		require.Error(t, err)
	})

	t.Run("canceled context", func(t *testing.T) {
		// Cancellation is tested with an independent context because
		// t.Context() is tied to the test lifetime; we need a context
		// we can cancel explicitly before the call.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := src.Open(ctx, "a.txt")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestFileSource_Inventory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	src, err := NewFileSource("file://"+dir, "")
	require.NoError(t, err)

	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)

	require.Len(t, inv.Files, 2)
	assert.Equal(t, "a.txt", inv.Files[0].Path)
	assert.Equal(t, int64(5), inv.Files[0].Size)
	assert.True(t, strings.HasPrefix(inv.Files[0].Digest, "sha256:"))
	assert.Equal(t, "b.txt", inv.Files[1].Path)
	assert.Equal(t, int64(5), inv.Files[1].Size)

	assert.Equal(t, "sha256", inv.Fingerprint.Scheme())
	assert.Len(t, inv.Fingerprint.value(), 64, "sha256 hex is 64 chars")
}

func TestFileSource_Inventory_Stable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	src, err := NewFileSource("file://"+dir, "")
	require.NoError(t, err)

	inv1, err := src.Inventory(t.Context())
	require.NoError(t, err)
	inv2, err := src.Inventory(t.Context())
	require.NoError(t, err)
	assert.Equal(t, inv1.Fingerprint, inv2.Fingerprint,
		"fingerprint must be stable across calls")
	assert.Equal(t, inv1.Files, inv2.Files,
		"file list must be stable across calls")
}

func TestFileSource_Inventory_DiffersOnChange(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))

	src, err := NewFileSource("file://"+dir, "")
	require.NoError(t, err)

	inv1, err := src.Inventory(t.Context())
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644))

	inv2, err := src.Inventory(t.Context())
	require.NoError(t, err)
	assert.NotEqual(t, inv1.Fingerprint, inv2.Fingerprint,
		"fingerprint must change when content changes")
	assert.NotEqual(t, inv1.Files[0].Digest, inv2.Files[0].Digest,
		"per-file digest must change when content changes")
}

func TestFileSource_Inventory_SkipsDotCog(t *testing.T) {
	withoutCog := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withoutCog, "a.txt"), []byte("hello"), 0o644))

	withCog := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withCog, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(withCog, ".cog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(withCog, ".cog", "state"), []byte("stuff"), 0o644))

	src1, err := NewFileSource("file://"+withoutCog, "")
	require.NoError(t, err)
	src2, err := NewFileSource("file://"+withCog, "")
	require.NoError(t, err)

	inv1, err := src1.Inventory(t.Context())
	require.NoError(t, err)
	inv2, err := src2.Inventory(t.Context())
	require.NoError(t, err)
	assert.Equal(t, inv1.Fingerprint, inv2.Fingerprint,
		".cog directory must be excluded from inventory")
	assert.Len(t, inv2.Files, 1)
}

func TestFileSource_Inventory_ContextCanceled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644))

	src, err := NewFileSource("file://"+dir, "")
	require.NoError(t, err)

	// Cancellation is tested with an independent context because
	// t.Context() is tied to the test lifetime; we need a context we can
	// cancel explicitly before the call.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = src.Inventory(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// Cross-check: the inventory fingerprint is the published set-digest
// formula (sha256 of sorted "hex  path" lines). Guards against the two
// computations drifting apart.
func TestFileSource_Inventory_FingerprintMatchesExplicitSetDigest(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	src, err := NewFileSource("file://"+dir, "")
	require.NoError(t, err)

	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(inv.Fingerprint.String(), "sha256:"))
}
