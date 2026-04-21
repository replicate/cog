package model

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeightLockEntry_JSONFieldNames(t *testing.T) {
	// Lockfile entries and the on-image /.cog/weights.json have the same
	// shape per spec §3.6. Tests that want to lock that down inspect the
	// JSON directly.
	entry := WeightLockEntry{
		Name:      "z-image-turbo",
		Target:    "/src/weights",
		Digest:    "sha256:abc",
		SetDigest: "sha256:def",
		Layers: []WeightLockLayer{
			{Digest: "sha256:aaa", Size: 15000000, MediaType: MediaTypeOCILayerTarGzip},
		},
	}
	require.Equal(t, "z-image-turbo", entry.Name)
	require.Equal(t, "/src/weights", entry.Target)
	require.Equal(t, "sha256:abc", entry.Digest)
	require.Equal(t, "sha256:def", entry.SetDigest)
	require.Len(t, entry.Layers, 1)
	require.Equal(t, MediaTypeOCILayerTarGzip, entry.Layers[0].MediaType)

	// Verify setDigest appears in JSON output.
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"setDigest":"sha256:def"`)
}

func TestMediaTypeArtifactConstant(t *testing.T) {
	require.Equal(t, "application/vnd.cog.weight.v1", MediaTypeWeightArtifact)
}

func TestMediaTypeWeightConfigConstant(t *testing.T) {
	require.Equal(t, "application/vnd.cog.weight.config.v1+json", MediaTypeWeightConfig)
}

func TestComputeWeightSetDigest_Deterministic(t *testing.T) {
	files := []PackedFile{
		{Path: "config.json", Size: 100, Digest: "sha256:aaa111", LayerDigest: "sha256:layer1"},
		{Path: "model.safetensors", Size: 9999, Digest: "sha256:bbb222", LayerDigest: "sha256:layer2"},
	}
	d1 := ComputeWeightSetDigest(files)
	d2 := ComputeWeightSetDigest(files)
	require.Equal(t, d1, d2, "same inputs must produce same digest")
	assert.True(t, len(d1) > len("sha256:"), "digest must be non-trivial")
}

func TestComputeWeightSetDigest_PackingIndependent(t *testing.T) {
	// Same files, different layer assignments → same set digest.
	files1 := []PackedFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:aaa", LayerDigest: "sha256:layer1"},
		{Path: "b.txt", Size: 20, Digest: "sha256:bbb", LayerDigest: "sha256:layer1"},
	}
	files2 := []PackedFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:aaa", LayerDigest: "sha256:layerX"},
		{Path: "b.txt", Size: 20, Digest: "sha256:bbb", LayerDigest: "sha256:layerY"},
	}
	assert.Equal(t, ComputeWeightSetDigest(files1), ComputeWeightSetDigest(files2),
		"set digest must be independent of layer assignment")
}

func TestComputeWeightSetDigest_DiffersForDifferentContent(t *testing.T) {
	files1 := []PackedFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:aaa"},
	}
	files2 := []PackedFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:bbb"},
	}
	assert.NotEqual(t, ComputeWeightSetDigest(files1), ComputeWeightSetDigest(files2),
		"different content must produce different set digest")
}

func TestBuildWeightConfigBlob_Deterministic(t *testing.T) {
	files := []PackedFile{
		{Path: "config.json", Size: 100, Digest: "sha256:aaa", LayerDigest: "sha256:l1"},
		{Path: "model.bin", Size: 9999, Digest: "sha256:bbb", LayerDigest: "sha256:l2"},
	}
	cfg1, sd1, err := BuildWeightConfigBlob("test-weight", "/src/weights", files)
	require.NoError(t, err)
	cfg2, sd2, err := BuildWeightConfigBlob("test-weight", "/src/weights", files)
	require.NoError(t, err)
	assert.Equal(t, cfg1, cfg2, "config blob must be deterministic")
	assert.Equal(t, sd1, sd2, "set digest must be deterministic")
}

func TestBuildWeightConfigBlob_Structure(t *testing.T) {
	files := []PackedFile{
		{Path: "config.json", Size: 100, Digest: "sha256:aaa", LayerDigest: "sha256:l1"},
		{Path: "model.bin", Size: 9999, Digest: "sha256:bbb", LayerDigest: "sha256:l2"},
	}
	configJSON, setDigest, err := BuildWeightConfigBlob("z-image-turbo", "/src/weights", files)
	require.NoError(t, err)

	var cfg WeightConfigBlob
	require.NoError(t, json.Unmarshal(configJSON, &cfg))

	assert.Equal(t, "z-image-turbo", cfg.Name)
	assert.Equal(t, "/src/weights", cfg.Target)
	assert.Equal(t, setDigest, cfg.SetDigest)
	require.Len(t, cfg.Files, 2)

	// Files should be sorted by path.
	assert.Equal(t, "config.json", cfg.Files[0].Path)
	assert.Equal(t, "model.bin", cfg.Files[1].Path)
	assert.Equal(t, int64(100), cfg.Files[0].Size)
	assert.Equal(t, "sha256:aaa", cfg.Files[0].Digest)
	assert.Equal(t, "sha256:l1", cfg.Files[0].Layer)
}

func TestBuildWeightConfigBlob_RejectsEmptyFiles(t *testing.T) {
	_, _, err := BuildWeightConfigBlob("name", "/target", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files")
}

func TestSetDigest_StableAcrossRepacks(t *testing.T) {
	// Pack the same directory twice with different thresholds (producing
	// different layers) and verify the set digest is identical.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	pr1, err := Pack(context.Background(), dir, &PackOptions{BundleFileMax: 1024, BundleSizeMax: 1024})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupLayerResults(pr1.Layers) })

	pr2, err := Pack(context.Background(), dir, &PackOptions{BundleFileMax: 1, BundleSizeMax: 1})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupLayerResults(pr2.Layers) })

	_, sd1, err := BuildWeightConfigBlob("w", "/w", pr1.Files)
	require.NoError(t, err)
	_, sd2, err := BuildWeightConfigBlob("w", "/w", pr2.Files)
	require.NoError(t, err)

	assert.Equal(t, sd1, sd2, "set digest must be stable across different packing strategies")
}

func TestConfigBlob_DiffersAcrossRepacks(t *testing.T) {
	// Different packing parameters → different config blobs (different
	// layer digests), but same set digest.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	pr1, err := Pack(context.Background(), dir, &PackOptions{BundleFileMax: 1024, BundleSizeMax: 1024})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupLayerResults(pr1.Layers) })

	// With BundleFileMax=1, all files are "large" (standalone layers).
	pr2, err := Pack(context.Background(), dir, &PackOptions{BundleFileMax: 1, BundleSizeMax: 1})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupLayerResults(pr2.Layers) })

	cfg1, _, err := BuildWeightConfigBlob("w", "/w", pr1.Files)
	require.NoError(t, err)
	cfg2, _, err := BuildWeightConfigBlob("w", "/w", pr2.Files)
	require.NoError(t, err)

	// Layer digests differ → config blobs differ.
	assert.NotEqual(t, cfg1, cfg2, "config blobs should differ when packing strategy differs")
}
