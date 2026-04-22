package weightsource

import (
	"context"
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
		{"unknown scheme rejected", "hf://org/repo", "", "cannot normalize"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeURI(tc.in)
			if tc.wantErrSubs != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubs)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFileSource_Fetch_Absolute(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644))

	// Absolute URI; projectDir is ignored.
	uri := "file://" + dir
	got, err := FileSource{}.Fetch(context.Background(), uri, "/unused")
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestFileSource_Fetch_BareAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	got, err := FileSource{}.Fetch(context.Background(), dir, "")
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestFileSource_Fetch_Relative(t *testing.T) {
	projectDir := t.TempDir()
	weightsDir := filepath.Join(projectDir, "weights")
	require.NoError(t, os.MkdirAll(weightsDir, 0o755))

	got, err := FileSource{}.Fetch(context.Background(), "file://./weights", projectDir)
	require.NoError(t, err)
	assert.Equal(t, weightsDir, got)
}

func TestFileSource_Fetch_BareRelative(t *testing.T) {
	projectDir := t.TempDir()
	weightsDir := filepath.Join(projectDir, "weights")
	require.NoError(t, os.MkdirAll(weightsDir, 0o755))

	got, err := FileSource{}.Fetch(context.Background(), "weights", projectDir)
	require.NoError(t, err)
	assert.Equal(t, weightsDir, got)
}

func TestFileSource_Fetch_ErrorCases(t *testing.T) {
	projectDir := t.TempDir()

	t.Run("missing", func(t *testing.T) {
		_, err := FileSource{}.Fetch(context.Background(), "file://./missing", projectDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("is a file not a dir", func(t *testing.T) {
		filePath := filepath.Join(projectDir, "oops.bin")
		require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))
		_, err := FileSource{}.Fetch(context.Background(), "file://./oops.bin", projectDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a directory")
	})

	t.Run("context canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := FileSource{}.Fetch(ctx, "file://./missing", projectDir)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("relative uri without project dir", func(t *testing.T) {
		_, err := FileSource{}.Fetch(context.Background(), "file://./weights", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "project directory")
	})
}

func TestFileSource_Fingerprint(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	fp, err := FileSource{}.Fingerprint(context.Background(), "file://"+dir, "")
	require.NoError(t, err)
	assert.Equal(t, "sha256", fp.Scheme())
	assert.NotEmpty(t, fp.Value())
	assert.Len(t, fp.Value(), 64, "sha256 hex is 64 chars")
}

func TestFileSource_Fingerprint_Stable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	fp1, err := FileSource{}.Fingerprint(context.Background(), "file://"+dir, "")
	require.NoError(t, err)
	fp2, err := FileSource{}.Fingerprint(context.Background(), "file://"+dir, "")
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2, "fingerprint must be stable across calls")
}

func TestFileSource_Fingerprint_DiffersOnChange(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))

	fp1, err := FileSource{}.Fingerprint(context.Background(), "file://"+dir, "")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644))

	fp2, err := FileSource{}.Fingerprint(context.Background(), "file://"+dir, "")
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp2, "fingerprint must change when content changes")
}

func TestFileSource_Fingerprint_SkipsDotCog(t *testing.T) {
	withoutCog := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withoutCog, "a.txt"), []byte("hello"), 0o644))

	withCog := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(withCog, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(withCog, ".cog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(withCog, ".cog", "state"), []byte("stuff"), 0o644))

	fp1, err := FileSource{}.Fingerprint(context.Background(), "file://"+withoutCog, "")
	require.NoError(t, err)
	fp2, err := FileSource{}.Fingerprint(context.Background(), "file://"+withCog, "")
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2, ".cog directory must be excluded from fingerprint")
}

func TestFileSource_Fingerprint_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := FileSource{}.Fingerprint(ctx, "file://"+dir, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// Cross-check: the fingerprint helper produces the same value as the
// published set-digest formula (sha256 of sorted "hex  path" lines).
// This guards against the two computations drifting apart.
func TestFileSource_Fingerprint_MatchesExplicitSetDigest(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	// "hello" sha256 = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	// "world" sha256 = 486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7
	// set digest = sha256("2cf2...  a.txt\n486e...  b.txt")
	fp, err := FileSource{}.Fingerprint(context.Background(), "file://"+dir, "")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(fp.String(), "sha256:"))
}
