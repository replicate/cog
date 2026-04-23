package model

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

// makeWeightDir writes files into <projectDir>/<relDir> and returns both
// absolute and relative paths. The contents are small enough to land in a
// single bundle layer under the default pack thresholds.
func makeWeightDir(t *testing.T, projectDir, relDir string, files map[string][]byte) {
	t.Helper()
	abs := filepath.Join(projectDir, relDir)
	require.NoError(t, os.MkdirAll(abs, 0o755))
	for name, data := range files {
		full := filepath.Join(abs, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, data, 0o644))
	}
}

func newTestBuilder(t *testing.T, projectDir string, weights []config.WeightSource) *WeightBuilder {
	t.Helper()
	src := NewSourceFromConfig(&config.Config{Weights: weights}, projectDir)
	lockPath := filepath.Join(projectDir, "weights.lock")
	return NewWeightBuilder(src, lockPath)
}

func TestWeightBuilder_HappyPath(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights/my-model", map[string][]byte{
		"config.json":    []byte(`{"hidden_size": 768}`),
		"tokenizer.json": []byte(`{"vocab_size": 50257}`),
	})

	wb := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "my-model", Target: "/src/weights/my-model", Source: &config.WeightSourceConfig{URI: "weights/my-model"}},
	})

	spec := NewWeightSpec("my-model", "weights/my-model", "/src/weights/my-model")
	artifact, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	wa, ok := artifact.(*WeightArtifact)
	require.True(t, ok, "expected *WeightArtifact, got %T", artifact)

	require.Equal(t, ArtifactTypeWeight, wa.Type())
	require.Equal(t, "my-model", wa.Name())
	require.Equal(t, "/src/weights/my-model", wa.Entry.Target)
	require.NotEmpty(t, wa.Entry.SetDigest, "builder should compute SetDigest")
	require.NotEmpty(t, wa.Entry.Files, "builder should populate Files")

	// At least one layer (the bundled small files).
	require.NotEmpty(t, wa.Layers, "expected at least one layer")
	for _, l := range wa.Layers {
		require.NotEmpty(t, l.TarPath, "layer should have a tar path")
		require.FileExists(t, l.TarPath, "layer tar should exist on disk")
		require.NotEmpty(t, l.Digest.Hex)
		require.Greater(t, l.Size, int64(0))
	}

	// Manifest descriptor should be populated without needing a registry.
	desc := wa.Descriptor()
	require.NotEmpty(t, desc.Digest.Hex)
	require.Greater(t, desc.Size, int64(0))
}

func TestWeightBuilder_WritesLockfile(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights/mw", map[string][]byte{
		"config.json":    []byte(`{"x": 1}`),
		"tokenizer.json": []byte(`{"y": 2}`),
	})

	wb := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "mw", Target: "/src/weights/mw", Source: &config.WeightSourceConfig{URI: "weights/mw"}},
	})

	spec := NewWeightSpec("mw", "weights/mw", "/src/weights/mw")
	artifact, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	wa := artifact.(*WeightArtifact)

	lockPath := filepath.Join(projectDir, "weights.lock")
	lock, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Equal(t, weightsLockVersion, lock.Version)
	require.Len(t, lock.Weights, 1)

	entry := lock.Weights[0]
	require.Equal(t, "mw", entry.Name)
	require.Equal(t, "/src/weights/mw", entry.Target)
	require.Equal(t, wa.Descriptor().Digest.String(), entry.Digest)
	require.Equal(t, wa.Entry.SetDigest, entry.SetDigest)
	require.Len(t, entry.Layers, len(wa.Layers))

	// Source block is populated with the normalized URI, a sha256
	// fingerprint, and empty include/exclude patterns.
	require.Equal(t, "file://./weights/mw", entry.Source.URI)
	require.Equal(t, "sha256", entry.Source.Fingerprint.Scheme())
	require.Equal(t, wa.Entry.SetDigest, entry.Source.Fingerprint.String(),
		"file:// fingerprint is the set digest")
	require.NotNil(t, entry.Source.Include)
	require.NotNil(t, entry.Source.Exclude)
	require.Empty(t, entry.Source.Include)
	require.Empty(t, entry.Source.Exclude)
	require.False(t, entry.Source.ImportedAt.IsZero())

	// File index is populated and sorted by path.
	require.Len(t, entry.Files, 2)
	require.Equal(t, "config.json", entry.Files[0].Path)
	require.Equal(t, "tokenizer.json", entry.Files[1].Path)
	for _, f := range entry.Files {
		require.NotEmpty(t, f.Digest)
		require.NotEmpty(t, f.Layer)
		require.Greater(t, f.Size, int64(0))
	}

	// Layer descriptors sorted by digest, carry compressed + uncompressed sizes.
	for i := 1; i < len(entry.Layers); i++ {
		require.Less(t, entry.Layers[i-1].Digest, entry.Layers[i].Digest,
			"layers must be sorted by digest")
	}
	for _, l := range entry.Layers {
		require.NotEmpty(t, l.Digest)
		require.NotEmpty(t, l.MediaType)
		require.Greater(t, l.Size, int64(0))
		require.Greater(t, l.SizeUncompressed, int64(0))
	}

	// Totals match sums.
	var wantSize, wantCompressed int64
	for _, l := range entry.Layers {
		wantSize += l.SizeUncompressed
		wantCompressed += l.Size
	}
	require.Equal(t, wantSize, entry.Size)
	require.Equal(t, wantCompressed, entry.SizeCompressed)
}

func TestWeightBuilder_UpdatesExistingLockfile(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w1", map[string][]byte{"a.json": []byte(`{"w":1}`)})
	makeWeightDir(t, projectDir, "w2", map[string][]byte{"b.json": []byte(`{"w":2}`)})

	wb := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w1", Target: "/src/w1", Source: &config.WeightSourceConfig{URI: "w1"}},
		{Name: "w2", Target: "/src/w2", Source: &config.WeightSourceConfig{URI: "w2"}},
	})

	_, err := wb.Build(context.Background(), NewWeightSpec("w1", "w1", "/src/w1"))
	require.NoError(t, err)
	_, err = wb.Build(context.Background(), NewWeightSpec("w2", "w2", "/src/w2"))
	require.NoError(t, err)

	lock, err := LoadWeightsLock(filepath.Join(projectDir, "weights.lock"))
	require.NoError(t, err)
	require.Len(t, lock.Weights, 2)

	names := map[string]bool{}
	for _, w := range lock.Weights {
		names[w.Name] = true
	}
	require.True(t, names["w1"])
	require.True(t, names["w2"])
}

func TestWeightBuilder_CacheHit(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: "w"}},
	})

	spec := NewWeightSpec("w", "w", "/src/w")
	first, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	fa := first.(*WeightArtifact)

	// Record the mtime of the first layer's tar file. Cache hits must not
	// rewrite the file, so the mtime should stay identical on the second
	// build.
	firstLayer := fa.Layers[0]
	originalInfo, err := os.Stat(firstLayer.TarPath)
	require.NoError(t, err)
	originalModTime := originalInfo.ModTime()

	second, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	sa := second.(*WeightArtifact)

	require.Equal(t, fa.Descriptor().Digest, sa.Descriptor().Digest,
		"cache hit should produce the same manifest digest")
	require.Len(t, sa.Layers, len(fa.Layers))
	for i, l := range sa.Layers {
		require.Equal(t, fa.Layers[i].Digest, l.Digest)
		require.Equal(t, fa.Layers[i].TarPath, l.TarPath,
			"cache hit should reuse the tar file on disk")
	}

	// Tar file should not have been rewritten.
	newInfo, err := os.Stat(firstLayer.TarPath)
	require.NoError(t, err)
	require.Equal(t, originalModTime, newInfo.ModTime(),
		"cache hit should not repack the layer")

	// Lockfile should not have been rewritten either — an unchanged entry
	// means no-op on disk. This matters for `cog weights push`, which
	// calls Build() and would otherwise churn the file every time.
	lockPath := filepath.Join(projectDir, "weights.lock")
	lockInfo, err := os.Stat(lockPath)
	require.NoError(t, err)

	thirdTime, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	_ = thirdTime

	newLockInfo, err := os.Stat(lockPath)
	require.NoError(t, err)
	require.Equal(t, lockInfo.ModTime(), newLockInfo.ModTime(),
		"cache hit should not rewrite weights.lock")

	lock, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Len(t, lock.Weights, 1)
}

func TestWeightBuilder_CacheHit_UpdatesConfigFields(t *testing.T) {
	// Config-driven fields (target, source URI) can change in cog.yaml
	// without the source content changing. The cache-hit path must stamp
	// the current values into the lockfile so `weights status` doesn't
	// report the weight as stale.
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	oldTarget := "/src/w"
	newTarget := "/src/w-moved"

	wb := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: oldTarget, Source: &config.WeightSourceConfig{URI: "w"}},
	})

	// First build writes the lockfile with the old target.
	spec := NewWeightSpec("w", "w", oldTarget)
	first, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	fa := first.(*WeightArtifact)

	lockPath := filepath.Join(projectDir, "weights.lock")
	lock, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Equal(t, oldTarget, lock.Weights[0].Target)
	require.Equal(t, "file://./w", lock.Weights[0].Source.URI)

	// Second build: same name, same source dir, different target and
	// different URI spelling. The tars are still on disk so the builder
	// should hit the cache and skip repacking, but it must update the
	// config-driven fields in the lockfile.
	spec2 := NewWeightSpec("w", "./w", newTarget)
	second, err := wb.Build(context.Background(), spec2)
	require.NoError(t, err)
	sa := second.(*WeightArtifact)

	// Layers should be reused (cache hit, no repack).
	require.Equal(t, fa.Layers[0].Digest, sa.Layers[0].Digest,
		"cache hit should reuse the same layers")

	// The lockfile must have the new target.
	lock2, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Len(t, lock2.Weights, 1)
	require.Equal(t, newTarget, lock2.Weights[0].Target,
		"cache-hit path must update the target in the lockfile")

	// The source URI normalizes identically for "w" and "./w", so it
	// should remain "file://./w". Verify it wasn't corrupted.
	require.Equal(t, "file://./w", lock2.Weights[0].Source.URI,
		"source URI must remain the normalized form")

	// Artifact should also carry the new target.
	require.Equal(t, newTarget, sa.Entry.Target)
}

func TestWeightBuilder_CacheHit_DoesNotRehashSource(t *testing.T) {
	// The cache-hit path reads the file index straight from the lockfile
	// instead of re-walking and re-hashing the source directory. Prove it
	// by corrupting the on-disk source after the first build: if the
	// builder still rehashed on every call, the set digest would change
	// and we'd rewrite the lockfile. It must not.
	projectDir := t.TempDir()
	weightDir := "w"
	makeWeightDir(t, projectDir, weightDir, map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: weightDir}},
	})
	spec := NewWeightSpec("w", weightDir, "/src/w")

	first, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	fa := first.(*WeightArtifact)

	lockPath := filepath.Join(projectDir, "weights.lock")
	lockInfoBefore, err := os.Stat(lockPath)
	require.NoError(t, err)

	// Replace the source file with different bytes. The cached tar is
	// still valid (size-matches), so the cache-hit path should fire and
	// reuse lockfile data without noticing the source drift. cog-wej9 is
	// where we'll add explicit check-for-drift.
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, weightDir, "a.json"),
		[]byte(`{"x":999}`), 0o644))

	second, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	sa := second.(*WeightArtifact)

	require.Equal(t, fa.Descriptor().Digest, sa.Descriptor().Digest,
		"cache-hit path must reuse lockfile data; manifest digest is unchanged")

	lockInfoAfter, err := os.Stat(lockPath)
	require.NoError(t, err)
	require.Equal(t, lockInfoBefore.ModTime(), lockInfoAfter.ModTime(),
		"cache-hit path must not rewrite the lockfile")
}

func TestWeightBuilder_CacheMiss_ContentsChanged(t *testing.T) {
	projectDir := t.TempDir()
	weightDir := "w"
	makeWeightDir(t, projectDir, weightDir, map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: &config.WeightSourceConfig{URI: weightDir}},
	})

	spec := NewWeightSpec("w", weightDir, "/src/w")
	first, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	fa := first.(*WeightArtifact)

	// Change the file content (different bytes => different digest).
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, weightDir, "a.json"),
		[]byte(`{"x":2,"y":3}`), 0o644))

	// Manually simulate cache invalidation: tar on disk has old contents so
	// its digest in the lockfile still matches the file. We need a way for
	// the builder to detect this. In the current simple implementation the
	// lockfile digest still matches the tar on disk, so the cache hits.
	// That's acceptable: changes to source files after a build require
	// either removing weights.lock or clearing the cache directory. Mimic
	// the latter and repack.
	require.NoError(t, os.RemoveAll(filepath.Join(projectDir, WeightsCacheDir)))

	second, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	sa := second.(*WeightArtifact)

	require.NotEqual(t, fa.Descriptor().Digest, sa.Descriptor().Digest,
		"repacked content should yield a different manifest digest")
}

func TestWeightBuilder_ErrorWrongSpecType(t *testing.T) {
	projectDir := t.TempDir()
	wb := newTestBuilder(t, projectDir, nil)

	imageSpec := NewImageSpec("model", "test-image")
	_, err := wb.Build(context.Background(), imageSpec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected *WeightSpec")
}

func TestWeightBuilder_ErrorSourceNotFound(t *testing.T) {
	projectDir := t.TempDir()
	wb := newTestBuilder(t, projectDir, nil)

	spec := NewWeightSpec("missing", "nonexistent-dir", "/src/missing")
	_, err := wb.Build(context.Background(), spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "weight source not found")
}

func TestWeightBuilder_ErrorSourceIsFile(t *testing.T) {
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "oops.bin"), []byte("data"), 0o644))

	wb := newTestBuilder(t, projectDir, nil)

	spec := NewWeightSpec("oops", "oops.bin", "/src/oops")
	_, err := wb.Build(context.Background(), spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a directory")
}

func TestWeightBuilder_ErrorContextCancelled(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb := newTestBuilder(t, projectDir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	spec := NewWeightSpec("w", "w", "/src/w")
	_, err := wb.Build(ctx, spec)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWeightBuilder_ImplementsBuilderInterface(t *testing.T) {
	projectDir := t.TempDir()
	wb := newTestBuilder(t, projectDir, nil)
	var _ Builder = wb
}

func TestWeightBuilder_NormalizesSourceURI(t *testing.T) {
	// Different bare-path spellings of the same directory should
	// produce the same normalized URI in the lockfile.
	tests := []struct {
		name    string
		rawURI  string
		wantURI string
	}{
		{"bare relative", "weights/mw", "file://./weights/mw"},
		{"dot prefix", "./weights/mw", "file://./weights/mw"},
		{"file scheme", "file://./weights/mw", "file://./weights/mw"},
		{"redundant slashes", "weights//mw", "file://./weights/mw"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			makeWeightDir(t, projectDir, "weights/mw", map[string][]byte{"c.json": []byte(`{}`)})

			wb := newTestBuilder(t, projectDir, []config.WeightSource{
				{Name: "mw", Target: "/src/weights/mw", Source: &config.WeightSourceConfig{URI: tc.rawURI}},
			})
			spec := NewWeightSpec("mw", tc.rawURI, "/src/weights/mw")
			_, err := wb.Build(context.Background(), spec)
			require.NoError(t, err)

			lock, err := LoadWeightsLock(filepath.Join(projectDir, "weights.lock"))
			require.NoError(t, err)
			require.Len(t, lock.Weights, 1)
			require.Equal(t, tc.wantURI, lock.Weights[0].Source.URI)
		})
	}
}
