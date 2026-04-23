package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model/weightsource"
)

func TestWeightLockEntry_JSONFieldNames(t *testing.T) {
	entry := WeightLockEntry{
		Name:      "z-image-turbo",
		Target:    "/src/weights",
		Digest:    "sha256:abc",
		SetDigest: "sha256:def",
		Source: WeightLockSource{
			URI:         "file://./weights",
			Fingerprint: weightsource.Fingerprint("sha256:def"),
			Include:     []string{},
			Exclude:     []string{},
		},
		Files: []WeightLockFile{
			{Path: "a.json", Size: 100, Digest: "sha256:f01", Layer: "sha256:aaa"},
		},
		Layers: []WeightLockLayer{
			{Digest: "sha256:aaa", MediaType: mediaTypeOCILayerTarGzip, Size: 110, SizeUncompressed: 100},
		},
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)
	s := string(data)

	// Sanity-check that every documented field name is present.
	for _, key := range []string{
		`"name":"z-image-turbo"`,
		`"target":"/src/weights"`,
		`"digest":"sha256:abc"`,
		`"setDigest":"sha256:def"`,
		`"source":`,
		`"uri":"file://./weights"`,
		`"fingerprint":"sha256:def"`,
		`"files":`,
		`"layers":`,
		`"sizeUncompressed":100`,
	} {
		assert.Contains(t, s, key, "expected field %q in JSON", key)
	}
}

func TestMediaTypeArtifactConstant(t *testing.T) {
	require.Equal(t, "application/vnd.cog.weight.v1", MediaTypeWeightArtifact)
}

func TestMediaTypeWeightConfigConstant(t *testing.T) {
	require.Equal(t, "application/vnd.cog.weight.config.v1+json", MediaTypeWeightConfig)
}

func TestComputeWeightSetDigest_Deterministic(t *testing.T) {
	files := []WeightLockFile{
		{Path: "config.json", Size: 100, Digest: "sha256:aaa111", Layer: "sha256:layer1"},
		{Path: "model.safetensors", Size: 9999, Digest: "sha256:bbb222", Layer: "sha256:layer2"},
	}
	d1 := computeWeightSetDigest(files)
	d2 := computeWeightSetDigest(files)
	require.Equal(t, d1, d2, "same inputs must produce same digest")
	assert.True(t, len(d1) > len("sha256:"), "digest must be non-trivial")
}

func TestComputeWeightSetDigest_PackingIndependent(t *testing.T) {
	// Same files, different layer assignments → same set digest.
	files1 := []WeightLockFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:aaa", Layer: "sha256:layer1"},
		{Path: "b.txt", Size: 20, Digest: "sha256:bbb", Layer: "sha256:layer1"},
	}
	files2 := []WeightLockFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:aaa", Layer: "sha256:layerX"},
		{Path: "b.txt", Size: 20, Digest: "sha256:bbb", Layer: "sha256:layerY"},
	}
	assert.Equal(t, computeWeightSetDigest(files1), computeWeightSetDigest(files2),
		"set digest must be independent of layer assignment")
}

func TestComputeWeightSetDigest_DiffersForDifferentContent(t *testing.T) {
	files1 := []WeightLockFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:aaa"},
	}
	files2 := []WeightLockFile{
		{Path: "a.txt", Size: 10, Digest: "sha256:bbb"},
	}
	assert.NotEqual(t, computeWeightSetDigest(files1), computeWeightSetDigest(files2),
		"different content must produce different set digest")
}

func TestBuildWeightConfigBlob_Deterministic(t *testing.T) {
	files := []WeightLockFile{
		{Path: "config.json", Size: 100, Digest: "sha256:aaa", Layer: "sha256:l1"},
		{Path: "model.bin", Size: 9999, Digest: "sha256:bbb", Layer: "sha256:l2"},
	}
	sd := computeWeightSetDigest(files)
	cfg1, err := buildWeightConfigBlob("test-weight", "/src/weights", sd, files)
	require.NoError(t, err)
	cfg2, err := buildWeightConfigBlob("test-weight", "/src/weights", sd, files)
	require.NoError(t, err)
	assert.Equal(t, cfg1, cfg2, "config blob must be deterministic")
}

func TestBuildWeightConfigBlob_Structure(t *testing.T) {
	files := []WeightLockFile{
		{Path: "config.json", Size: 100, Digest: "sha256:aaa", Layer: "sha256:l1"},
		{Path: "model.bin", Size: 9999, Digest: "sha256:bbb", Layer: "sha256:l2"},
	}
	setDigest := computeWeightSetDigest(files)
	configJSON, err := buildWeightConfigBlob("z-image-turbo", "/src/weights", setDigest, files)
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
	_, err := buildWeightConfigBlob("name", "/target", "sha256:000", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files")
}

func TestSetDigest_StableAcrossRepacks(t *testing.T) {
	// Pack the same directory twice with different thresholds (producing
	// different layers) and verify the set digest is identical.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	pr1, err := packTestDir(t, dir, &packOptions{BundleFileMax: 1024, BundleSizeMax: 1024})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupPackedLayers(pr1.Layers) })

	pr2, err := packTestDir(t, dir, &packOptions{BundleFileMax: 1, BundleSizeMax: 1})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupPackedLayers(pr2.Layers) })

	entry1 := newWeightLockEntry("w", "/w", WeightLockSource{}, pr1.Files, pr1.Layers)
	entry2 := newWeightLockEntry("w", "/w", WeightLockSource{}, pr2.Files, pr2.Layers)

	assert.Equal(t, entry1.SetDigest, entry2.SetDigest,
		"set digest must be stable across different packing strategies")
}

func TestConfigBlob_DiffersAcrossRepacks(t *testing.T) {
	// Different packing parameters → different config blobs (different
	// layer digests), but same set digest.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	pr1, err := packTestDir(t, dir, &packOptions{BundleFileMax: 1024, BundleSizeMax: 1024})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupPackedLayers(pr1.Layers) })

	// With BundleFileMax=1, all files are "large" (standalone layers).
	pr2, err := packTestDir(t, dir, &packOptions{BundleFileMax: 1, BundleSizeMax: 1})
	require.NoError(t, err)
	t.Cleanup(func() { cleanupPackedLayers(pr2.Layers) })

	entry1 := newWeightLockEntry("w", "/w", WeightLockSource{}, pr1.Files, pr1.Layers)
	entry2 := newWeightLockEntry("w", "/w", WeightLockSource{}, pr2.Files, pr2.Layers)

	cfg1, err := buildWeightConfigBlob("w", "/w", entry1.SetDigest, entry1.Files)
	require.NoError(t, err)
	cfg2, err := buildWeightConfigBlob("w", "/w", entry2.SetDigest, entry2.Files)
	require.NoError(t, err)

	// Layer digests differ → config blobs differ.
	assert.NotEqual(t, cfg1, cfg2, "config blobs should differ when packing strategy differs")
}

func TestNewWeightLockEntry_PopulatesFromPackResult(t *testing.T) {
	layers := []packedLayer{
		{
			Digest:           v1.Hash{Algorithm: "sha256", Hex: "aaa"},
			Size:             110,
			UncompressedSize: 100,
			MediaType:        mediaTypeOCILayerTarGzip,
		},
		{
			Digest:           v1.Hash{Algorithm: "sha256", Hex: "bbb"},
			Size:             2000,
			UncompressedSize: 2000,
			MediaType:        mediaTypeOCILayerTar,
		},
	}
	files := []packedFile{
		{Path: "a.json", Size: 100, Digest: "sha256:f01", LayerDigest: "sha256:aaa"},
		{Path: "b.bin", Size: 2000, Digest: "sha256:f02", LayerDigest: "sha256:bbb"},
	}
	src := WeightLockSource{
		URI:         "file://./weights",
		Fingerprint: weightsource.Fingerprint("sha256:setdigest"),
		Include:     []string{},
		Exclude:     []string{},
		ImportedAt:  time.Date(2026, 4, 16, 17, 27, 7, 0, time.UTC),
	}

	entry := newWeightLockEntry("w", "/src/w", src, files, layers)

	assert.Equal(t, "w", entry.Name)
	assert.Equal(t, "/src/w", entry.Target)
	assert.Empty(t, entry.Digest, "Digest should be empty (filled by caller after manifest build)")
	assert.NotEmpty(t, entry.SetDigest, "SetDigest should be computed internally")

	// Size = sum of uncompressed; SizeCompressed = sum of layer sizes.
	assert.Equal(t, int64(100+2000), entry.Size)
	assert.Equal(t, int64(110+2000), entry.SizeCompressed)

	require.Len(t, entry.Files, 2)
	require.Len(t, entry.Layers, 2)
	assert.Equal(t, src, entry.Source)

	// Files sorted by path, layers sorted by digest.
	assert.Equal(t, "a.json", entry.Files[0].Path)
	assert.Equal(t, "sha256:aaa", entry.Layers[0].Digest)
}

// TestSetDigest_CrossPath verifies that the packer-based set digest
// (computeWeightSetDigest over WeightLockFile) and the weightsource-based
// fingerprint (computeInventory via FileSource.Inventory) produce the
// same value for the same directory. If either formula drifts, this
// test catches it.
func TestSetDigest_CrossPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644))

	// Path 1: pack, convert to lock entry, compute from WeightLockFile slice.
	pr, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupPackedLayers(pr.Layers) })
	entry := newWeightLockEntry("w", "/w", WeightLockSource{}, pr.Files, pr.Layers)
	packerSetDigest := computeWeightSetDigest(entry.Files)

	// Path 2: inventory fingerprint from directory walk (the weightsource path).
	src, err := weightsource.NewFileSource("file://"+dir, "")
	require.NoError(t, err)
	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)

	assert.Equal(t, packerSetDigest, inv.Fingerprint.String(),
		"packer and weightsource must produce the same set digest for the same directory")
}
