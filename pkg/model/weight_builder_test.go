package model

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
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

// newTestBuilder constructs a WeightBuilder rooted in projectDir with a
// fresh FileStore in t.TempDir() — the canonical fixture for builder
// tests. Returns the builder and the store so tests that need to
// inspect or pre-populate the store can reach it.
func newTestBuilder(t *testing.T, projectDir string, weights []config.WeightSource) (*WeightBuilder, *store.FileStore) {
	t.Helper()
	src := NewSourceFromConfig(&config.Config{Weights: weights}, projectDir)
	st, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	lockPath := filepath.Join(projectDir, "weights.lock")
	return NewWeightBuilder(src, st, lockPath), st
}

func testWeightSpec(t *testing.T, name, uri, target string) *WeightSpec {
	t.Helper()
	spec, err := WeightSpecFromConfig(config.WeightSource{
		Name:   name,
		Target: target,
		Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: uri}}},
	})
	require.NoError(t, err)
	return spec
}

func TestWeightBuilder_HappyPath(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights/my-model", map[string][]byte{
		"config.json":    []byte(`{"hidden_size": 768}`),
		"tokenizer.json": []byte(`{"vocab_size": 50257}`),
	})

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "my-model", Target: "/src/weights/my-model", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "weights/my-model"}}}},
	})

	spec := testWeightSpec(t, "my-model", "weights/my-model", "/src/weights/my-model")
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
		require.NotEmpty(t, l.Digest.Hex)
		require.Greater(t, l.Size, int64(0))
		require.NotEmpty(t, l.Plan.Files,
			"layer should retain its plan for streaming on push")
	}

	// Manifest descriptor should be populated without needing a registry.
	desc := wa.Descriptor()
	require.NotEmpty(t, desc.Digest.Hex)
	require.Greater(t, desc.Size, int64(0))
}

func TestWeightBuilder_PopulatesStore(t *testing.T) {
	// Core promise of cog-i12u: after Build, every file from the
	// inventory exists in the local content store. cog predict can
	// then hardlink-assemble without a separate `cog weights pull`.
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{
		"a.json": []byte(`{"x":1}`),
		"b.json": []byte(`{"y":2}`),
	})

	wb, st := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
	})

	spec := testWeightSpec(t, "w", "w", "/src/w")
	art, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	wa := art.(*WeightArtifact)

	for _, f := range wa.Entry.Files {
		ok, err := st.Exists(context.Background(), f.Digest)
		require.NoError(t, err)
		assert.True(t, ok, "file %s (%s) should be in the store after Build", f.Path, f.Digest)
	}
}

func TestWeightBuilder_FastPath_PopulatesEmptyStore(t *testing.T) {
	// Scenario: the lockfile is present (e.g. checked into git) but
	// the local store is cold (e.g. fresh clone, or a brand-new
	// machine). Build must still ingress every file into the store
	// — so `cog predict` works without a separate `cog weights
	// pull` even on the fast path.
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{
		"a.json": []byte(`{"x":1}`),
		"b.json": []byte(`{"y":2}`),
	})

	// First, do a normal build to write the lockfile.
	wb1, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
	})
	spec := testWeightSpec(t, "w", "w", "/src/w")
	_, err := wb1.Build(context.Background(), spec)
	require.NoError(t, err)

	// Now: same project, same lockfile on disk, but a brand-new
	// (empty) store. This is the "fresh clone" scenario.
	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
		},
	}, projectDir)
	freshStore, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	wb2 := NewWeightBuilder(src, freshStore, filepath.Join(projectDir, "weights.lock"))
	art, err := wb2.Build(context.Background(), spec)
	require.NoError(t, err)
	wa := art.(*WeightArtifact)

	// Every file in the lockfile must now be in the cold store too.
	for _, f := range wa.Entry.Files {
		ok, err := freshStore.Exists(context.Background(), f.Digest)
		require.NoError(t, err)
		assert.True(t, ok,
			"fast-path build with cold store must populate file %s (%s)",
			f.Path, f.Digest)
	}
}

func TestWeightBuilder_StampsEnvelopeFormat(t *testing.T) {
	// Every successful Build must stamp the current envelope format
	// into the lockfile. This is the field that lets future imports
	// detect cog-version drift in packer behavior.
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
	})

	spec := testWeightSpec(t, "w", "w", "/src/w")
	_, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	lock, err := lockfile.LoadWeightsLock(filepath.Join(projectDir, "weights.lock"))
	require.NoError(t, err)

	want, err := computeEnvelopeFormat(envelopeFromOptions(packOptions{}))
	require.NoError(t, err)
	assert.Equal(t, want, lock.EnvelopeFormat,
		"lockfile must stamp the current envelope format")
}

func TestWeightBuilder_EnvelopeFormatMismatch_TriggersRecompute(t *testing.T) {
	// If the lockfile's recorded EnvelopeFormat doesn't match the
	// current envelope (e.g. after a cog upgrade with a packer
	// behavior change), Build must recompute layer digests rather
	// than trust the lockfile's recorded values. Simulate the drift
	// by writing a stale envelopeFormat into the lockfile and
	// confirm Build rewrites it to the current value.
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
	})
	spec := testWeightSpec(t, "w", "w", "/src/w")
	_, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	lockPath := filepath.Join(projectDir, "weights.lock")

	// Corrupt the recorded EnvelopeFormat on disk.
	lock, err := lockfile.LoadWeightsLock(lockPath)
	require.NoError(t, err)
	lock.EnvelopeFormat = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	require.NoError(t, lock.Save(lockPath))

	// Rebuild — recompute path should fire and stamp the correct
	// envelope.
	_, err = wb.Build(context.Background(), spec)
	require.NoError(t, err)

	lock, err = lockfile.LoadWeightsLock(lockPath)
	require.NoError(t, err)
	want, err := computeEnvelopeFormat(envelopeFromOptions(packOptions{}))
	require.NoError(t, err)
	assert.Equal(t, want, lock.EnvelopeFormat,
		"recompute path must stamp the current envelope format")
}

func TestWeightBuilder_FastPath_NoOpRebuild(t *testing.T) {
	// Build the same source twice. Second build's source fingerprint
	// matches the lockfile's recorded fingerprint, so canFastPath
	// returns true and Build trusts the recorded layer digests
	// without recomputing. The lockfile's mtime stays put (no write
	// since EntriesEqual returns true), and the manifest digest is
	// identical to the first build's.
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
	})
	spec := testWeightSpec(t, "w", "w", "/src/w")
	first, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	fa := first.(*WeightArtifact)

	lockPath := filepath.Join(projectDir, "weights.lock")
	infoBefore, err := os.Stat(lockPath)
	require.NoError(t, err)

	second, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	sa := second.(*WeightArtifact)

	assert.Equal(t, fa.Descriptor().Digest, sa.Descriptor().Digest,
		"unchanged source must produce identical manifest digest")

	infoAfter, err := os.Stat(lockPath)
	require.NoError(t, err)
	assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime(),
		"unchanged-source rebuild must not rewrite weights.lock")
}

func TestWeightBuilder_WritesLockfile(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "weights/mw", map[string][]byte{
		"config.json":    []byte(`{"x": 1}`),
		"tokenizer.json": []byte(`{"y": 2}`),
	})

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "mw", Target: "/src/weights/mw", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "weights/mw"}}}},
	})

	spec := testWeightSpec(t, "mw", "weights/mw", "/src/weights/mw")
	artifact, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	wa := artifact.(*WeightArtifact)

	lockPath := filepath.Join(projectDir, "weights.lock")
	lock, err := lockfile.LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Equal(t, lockfile.Version, lock.Version)
	require.Len(t, lock.Weights, 1)

	entry := lock.Weights[0]
	require.Equal(t, "mw", entry.Name)
	require.Equal(t, "/src/weights/mw", entry.Target)
	require.Equal(t, wa.Descriptor().Digest.String(), entry.Digest)
	require.Equal(t, wa.Entry.SetDigest, entry.SetDigest)
	require.Len(t, entry.Layers, len(wa.Layers))

	// Source block is populated with the normalized URI, a sha256
	// fingerprint, and empty include/exclude patterns.
	require.Len(t, entry.Sources, 1)
	require.Equal(t, "file://./weights/mw", entry.Sources[0].URI)
	require.Equal(t, "sha256", entry.Sources[0].Fingerprint.Scheme())
	require.Equal(t, wa.Entry.SetDigest, entry.Sources[0].Fingerprint.String(),
		"file:// fingerprint is the set digest")
	require.NotNil(t, entry.Sources[0].Include)
	require.NotNil(t, entry.Sources[0].Exclude)
	require.Empty(t, entry.Sources[0].Include)
	require.Empty(t, entry.Sources[0].Exclude)
	require.False(t, entry.ImportedAt.IsZero())

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

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w1", Target: "/src/w1", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w1"}}}},
		{Name: "w2", Target: "/src/w2", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w2"}}}},
	})

	_, err := wb.Build(context.Background(), testWeightSpec(t, "w1", "w1", "/src/w1"))
	require.NoError(t, err)
	_, err = wb.Build(context.Background(), testWeightSpec(t, "w2", "w2", "/src/w2"))
	require.NoError(t, err)

	lock, err := lockfile.LoadWeightsLock(filepath.Join(projectDir, "weights.lock"))
	require.NoError(t, err)
	require.Len(t, lock.Weights, 2)

	names := map[string]bool{}
	for _, w := range lock.Weights {
		names[w.Name] = true
	}
	require.True(t, names["w1"])
	require.True(t, names["w2"])
}

func TestWeightBuilder_FastPath_UpdatesConfigFields(t *testing.T) {
	// Config-driven fields (target, source URI) can change in
	// cog.yaml without the source content changing. The fast path
	// must stamp the current values into the lockfile so weights
	// status doesn't report the weight as stale.
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	oldTarget := "/src/w"
	newTarget := "/src/w-moved"

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: oldTarget, Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
	})

	// First build writes the lockfile with the old target.
	spec := testWeightSpec(t, "w", "w", oldTarget)
	first, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	fa := first.(*WeightArtifact)

	lockPath := filepath.Join(projectDir, "weights.lock")
	lock, err := lockfile.LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Equal(t, oldTarget, lock.Weights[0].Target)
	require.Equal(t, "file://./w", lock.Weights[0].Sources[0].URI)

	// Second build: same name, same source dir, different target.
	// Layers should be reused (fast path) but the target must be
	// stamped into the lockfile.
	spec2 := testWeightSpec(t, "w", "./w", newTarget)
	second, err := wb.Build(context.Background(), spec2)
	require.NoError(t, err)
	sa := second.(*WeightArtifact)

	// Layers reused via fast path.
	require.Equal(t, fa.Layers[0].Digest, sa.Layers[0].Digest,
		"fast path should reuse the same layers")

	lock2, err := lockfile.LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Len(t, lock2.Weights, 1)
	require.Equal(t, newTarget, lock2.Weights[0].Target,
		"fast-path rebuild must update the target in the lockfile")

	require.Equal(t, "file://./w", lock2.Weights[0].Sources[0].URI,
		"normalized source URI must be preserved")
	require.Equal(t, newTarget, sa.Entry.Target)
}

func TestWeightBuilder_CacheMiss_ContentsChanged(t *testing.T) {
	projectDir := t.TempDir()
	weightDir := "w"
	makeWeightDir(t, projectDir, weightDir, map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: weightDir}}}},
	})

	spec := testWeightSpec(t, "w", weightDir, "/src/w")
	first, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	fa := first.(*WeightArtifact)

	// Change the file content (different bytes => different digest).
	// canFastPath detects this through Source.Fingerprint mismatch
	// (fingerprint is the dirhash of the file set for file://) and
	// falls back to recompute.
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, weightDir, "a.json"),
		[]byte(`{"x":2,"y":3}`), 0o644))

	second, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	sa := second.(*WeightArtifact)

	require.NotEqual(t, fa.Descriptor().Digest, sa.Descriptor().Digest,
		"changed content should yield a different manifest digest")
}

func TestWeightBuilder_ErrorWrongSpecType(t *testing.T) {
	projectDir := t.TempDir()
	wb, _ := newTestBuilder(t, projectDir, nil)

	imageSpec := NewImageSpec("model", "test-image")
	_, err := wb.Build(context.Background(), imageSpec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected *WeightSpec")
}

func TestWeightBuilder_ErrorSourceNotFound(t *testing.T) {
	projectDir := t.TempDir()
	wb, _ := newTestBuilder(t, projectDir, nil)

	spec := testWeightSpec(t, "missing", "nonexistent-dir", "/src/missing")
	_, err := wb.Build(context.Background(), spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "weight source not found")
}

func TestWeightBuilder_ErrorSourceIsFile(t *testing.T) {
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "oops.bin"), []byte("data"), 0o644))

	wb, _ := newTestBuilder(t, projectDir, nil)

	spec := testWeightSpec(t, "oops", "oops.bin", "/src/oops")
	_, err := wb.Build(context.Background(), spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not a directory")
}

func TestWeightBuilder_ErrorContextCancelled(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "w", map[string][]byte{"a.json": []byte(`{"x":1}`)})

	wb, _ := newTestBuilder(t, projectDir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	spec := testWeightSpec(t, "w", "w", "/src/w")
	_, err := wb.Build(ctx, spec)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWeightBuilder_ImplementsBuilderInterface(t *testing.T) {
	projectDir := t.TempDir()
	wb, _ := newTestBuilder(t, projectDir, nil)
	var _ Builder = wb
}

func TestWeightBuilder_IdenticalContentDifferentPaths(t *testing.T) {
	// Regression test: two files with identical content but different
	// paths must produce distinct layers (tar headers include the path).
	// Previously layerKey only used digests, so both files mapped to
	// the same key and one plan overwrote the other in the lookup map.
	projectDir := t.TempDir()
	sameContent := []byte("identical-bytes-for-both-files")
	makeWeightDir(t, projectDir, "w", map[string][]byte{
		"a.bin": sameContent,
		"b.bin": sameContent,
	})

	wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
		{Name: "w", Target: "/src/w", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: "w"}}}},
	})

	spec := testWeightSpec(t, "w", "w", "/src/w")
	art, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	wa := art.(*WeightArtifact)

	// Both files should appear in the lockfile entry.
	require.Len(t, wa.Entry.Files, 2, "both files must be tracked")
	paths := []string{wa.Entry.Files[0].Path, wa.Entry.Files[1].Path}
	assert.Contains(t, paths, "a.bin")
	assert.Contains(t, paths, "b.bin")

	// A second build should also succeed (fast path), confirming
	// layersFromLockfile correctly pairs layers when content is
	// identical but paths differ.
	art2, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	wa2 := art2.(*WeightArtifact)
	assert.Equal(t, wa.Descriptor().Digest, wa2.Descriptor().Digest,
		"fast-path rebuild must produce the same manifest digest")
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

			wb, _ := newTestBuilder(t, projectDir, []config.WeightSource{
				{Name: "mw", Target: "/src/weights/mw", Source: config.WeightSourceList{Items: []config.WeightSourceConfig{{URI: tc.rawURI}}}},
			})
			spec := testWeightSpec(t, "mw", tc.rawURI, "/src/weights/mw")
			_, err := wb.Build(context.Background(), spec)
			require.NoError(t, err)

			lock, err := lockfile.LoadWeightsLock(filepath.Join(projectDir, "weights.lock"))
			require.NoError(t, err)
			require.Len(t, lock.Weights, 1)
			require.Equal(t, tc.wantURI, lock.Weights[0].Sources[0].URI)
		})
	}
}

// TestWeightBuilder_MultiSourceMerge covers two-source resolution:
// disjoint files merge into one inventory; overlapping paths resolve
// last-in-wins (declaration order = layering order). Both scenarios
// share the same setup shape, so they table-drive cleanly.
//
// In every case Sources[] preserves declaration order with distinct
// per-source fingerprints; the per-case expectations focus on the
// merged-file index and the bytes the store ends up holding.
func TestWeightBuilder_MultiSourceMerge(t *testing.T) {
	bytesA := []byte(`{"version": "from-a"}`)
	bytesB := []byte(`{"version": "from-b"}`)

	tests := []struct {
		name       string
		aFiles     map[string][]byte
		bFiles     map[string][]byte
		wantPaths  []string
		wantWinner map[string][]byte // path → bytes that should land in the store
	}{
		{
			name:       "disjoint files merge into both",
			aFiles:     map[string][]byte{"config.json": []byte(`{"hidden_size": 768}`)},
			bFiles:     map[string][]byte{"tokenizer.json": []byte(`{"vocab_size": 50257}`)},
			wantPaths:  []string{"config.json", "tokenizer.json"},
			wantWinner: nil, // no overlap; skip byte check
		},
		{
			name:       "overlapping path: last source wins",
			aFiles:     map[string][]byte{"config.json": bytesA},
			bFiles:     map[string][]byte{"config.json": bytesB},
			wantPaths:  []string{"config.json"},
			wantWinner: map[string][]byte{"config.json": bytesB},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			makeWeightDir(t, projectDir, "src-a", tc.aFiles)
			makeWeightDir(t, projectDir, "src-b", tc.bFiles)

			weights := []config.WeightSource{{
				Name:   "w",
				Target: "/src/w",
				Source: config.WeightSourceList{Items: []config.WeightSourceConfig{
					{URI: "src-a"}, {URI: "src-b"},
				}},
			}}
			wb, st := newTestBuilder(t, projectDir, weights)
			spec, err := WeightSpecFromConfig(weights[0])
			require.NoError(t, err)

			artifact, err := wb.Build(context.Background(), spec)
			require.NoError(t, err)
			wa := artifact.(*WeightArtifact)

			gotPaths := make([]string, len(wa.Entry.Files))
			for i, f := range wa.Entry.Files {
				gotPaths[i] = f.Path
			}
			assert.Equal(t, tc.wantPaths, gotPaths, "merged file index")

			require.Len(t, wa.Entry.Sources, 2)
			assert.Equal(t, "file://./src-a", wa.Entry.Sources[0].URI)
			assert.Equal(t, "file://./src-b", wa.Entry.Sources[1].URI)
			assert.NotEqual(t, wa.Entry.Sources[0].Fingerprint, wa.Entry.Sources[1].Fingerprint,
				"distinct sources must hash differently")

			for path, want := range tc.wantWinner {
				idx := slices.IndexFunc(wa.Entry.Files, func(f lockfile.WeightLockFile) bool {
					return f.Path == path
				})
				require.GreaterOrEqual(t, idx, 0, "expected file %s in entry", path)
				storePath, err := st.Path(context.Background(), wa.Entry.Files[idx].Digest)
				require.NoError(t, err)
				got, err := os.ReadFile(storePath) //nolint:gosec // G304: storePath is from the test's own FileStore
				require.NoError(t, err)
				assert.Equal(t, want, got, "store bytes for %s", path)
			}
		})
	}
}

// TestWeightBuilder_FingerprintDriftPerSource verifies that mutating
// one source's contents updates only that source's fingerprint,
// leaving the other stable.
func TestWeightBuilder_FingerprintDriftPerSource(t *testing.T) {
	projectDir := t.TempDir()
	makeWeightDir(t, projectDir, "src-a", map[string][]byte{"a.json": []byte(`{"v":1}`)})
	makeWeightDir(t, projectDir, "src-b", map[string][]byte{"b.json": []byte(`{"v":1}`)})

	weights := []config.WeightSource{{
		Name:   "w",
		Target: "/src/w",
		Source: config.WeightSourceList{Items: []config.WeightSourceConfig{
			{URI: "src-a"}, {URI: "src-b"},
		}},
	}}
	wb, _ := newTestBuilder(t, projectDir, weights)
	spec, err := WeightSpecFromConfig(weights[0])
	require.NoError(t, err)

	_, err = wb.Build(context.Background(), spec)
	require.NoError(t, err)

	lockPath := filepath.Join(projectDir, "weights.lock")
	lock1, err := lockfile.LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Len(t, lock1.Weights[0].Sources, 2)
	fpA1 := lock1.Weights[0].Sources[0].Fingerprint
	fpB1 := lock1.Weights[0].Sources[1].Fingerprint

	makeWeightDir(t, projectDir, "src-b", map[string][]byte{"b.json": []byte(`{"v":2}`)})

	_, err = wb.Build(context.Background(), spec)
	require.NoError(t, err)

	lock2, err := lockfile.LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Len(t, lock2.Weights[0].Sources, 2)
	assert.Equal(t, fpA1, lock2.Weights[0].Sources[0].Fingerprint,
		"untouched source's fingerprint must stay stable")
	assert.NotEqual(t, fpB1, lock2.Weights[0].Sources[1].Fingerprint,
		"mutated source's fingerprint must change")
}
