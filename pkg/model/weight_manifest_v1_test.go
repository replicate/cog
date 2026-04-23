package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model/weightsource"
)

// =============================================================================
// Helpers
// =============================================================================

// packDir runs Pack on sourceDir and registers cleanup of the produced tar files.
func packDir(t *testing.T, sourceDir string, opts *packOptions) []packedLayer {
	t.Helper()
	src, err := weightsource.NewFileSource("file://"+sourceDir, "")
	require.NoError(t, err)
	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)
	results, err := newPacker(opts).pack(t.Context(), src, inv)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, r := range results.Layers {
			_ = os.Remove(r.TarPath)
		}
	})
	return results.Layers
}

// writeSrcFile writes size bytes at relPath under dir.
func writeSrcFile(t *testing.T, dir, relPath string, size int64) {
	t.Helper()
	abs := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	f, err := os.Create(abs) //nolint:gosec // test file
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck
	if size > 0 {
		require.NoError(t, f.Truncate(size))
	}
}

// defaultEntry returns a minimal valid WeightLockEntry for manifest tests.
func defaultEntry() WeightLockEntry {
	return WeightLockEntry{
		Name:      "z-image-turbo",
		Target:    "/src/weights",
		SetDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Files: []WeightLockFile{
			{Path: "config.json", Size: 128, Digest: "sha256:aaa", Layer: "sha256:layer1"},
		},
	}
}

// singleSmallFileLayers produces a valid single-layer result set for tests that
// only care about manifest shape, not layer contents.
func singleSmallFileLayers(t *testing.T) []packedLayer {
	t.Helper()
	dir := t.TempDir()
	writeSrcFile(t, dir, "config.json", 128)
	return packDir(t, dir, nil)
}

// =============================================================================
// Entry validation via buildWeightManifestV1
// =============================================================================

func TestBuildWeightManifestV1_RejectsInvalidEntry(t *testing.T) {
	validSetDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	validFiles := []WeightLockFile{{Path: "f.bin", Size: 1, Digest: "sha256:aaa", Layer: "sha256:l1"}}
	layers := singleSmallFileLayers(t)

	tests := []struct {
		name    string
		entry   WeightLockEntry
		wantErr string
	}{
		{"missing name", WeightLockEntry{Target: "/x", SetDigest: validSetDigest, Files: validFiles}, "weight name is required"},
		{"missing target", WeightLockEntry{Name: "n", SetDigest: validSetDigest, Files: validFiles}, "weight target is required"},
		{"missing set digest", WeightLockEntry{Name: "n", Target: "/x", Files: validFiles}, "weight set digest is required"},
		{"missing files", WeightLockEntry{Name: "n", Target: "/x", SetDigest: validSetDigest}, "weight files are required"},
		{"valid", WeightLockEntry{Name: "n", Target: "/x", SetDigest: validSetDigest, Files: validFiles}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildWeightManifestV1(tc.entry, layers)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestBuildWeightManifestV1_ManifestAnnotations(t *testing.T) {
	layers := singleSmallFileLayers(t)
	entry := defaultEntry()
	img, err := buildWeightManifestV1(entry, layers)
	require.NoError(t, err)

	m, err := img.Manifest()
	require.NoError(t, err)

	assert.Equal(t, "z-image-turbo", m.Annotations[AnnotationV1WeightName])
	assert.Equal(t, "/src/weights", m.Annotations[AnnotationV1WeightTarget])
	assert.Equal(t, "sha256:0000000000000000000000000000000000000000000000000000000000000000", m.Annotations[AnnotationV1WeightSetDigest])
}

// =============================================================================
// buildWeightManifestV1 — validation
// =============================================================================

func TestBuildWeightManifestV1_RejectsMissingName(t *testing.T) {
	layers := singleSmallFileLayers(t)

	_, err := buildWeightManifestV1(WeightLockEntry{
		Target:    "/x",
		SetDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Files:     []WeightLockFile{{Path: "f", Size: 1, Digest: "sha256:a", Layer: "sha256:l"}},
	}, layers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestBuildWeightManifestV1_RejectsEmptyLayers(t *testing.T) {
	_, err := buildWeightManifestV1(defaultEntry(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one layer")
}

func TestBuildWeightManifestV1_RejectsInvalidLayer(t *testing.T) {
	base := singleSmallFileLayers(t)

	cases := []struct {
		name    string
		mutate  func(lr *packedLayer)
		wantErr string
	}{
		{"missing TarPath", func(lr *packedLayer) { lr.TarPath = "" }, "missing TarPath"},
		{"missing digest", func(lr *packedLayer) { lr.Digest = v1.Hash{} }, "missing digest"},
		{"zero size", func(lr *packedLayer) { lr.Size = 0 }, "invalid size"},
		{"missing media type", func(lr *packedLayer) { lr.MediaType = "" }, "missing media type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lr := base[0]
			tc.mutate(&lr)
			_, err := buildWeightManifestV1(defaultEntry(), []packedLayer{lr})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// =============================================================================
// buildWeightManifestV1 — manifest structure
// =============================================================================

func TestBuildWeightManifestV1_ManifestShape(t *testing.T) {
	layers := singleSmallFileLayers(t)
	entry := defaultEntry()
	img, err := buildWeightManifestV1(entry, layers)
	require.NoError(t, err)

	// Manifest schema and media type.
	m, err := img.Manifest()
	require.NoError(t, err)
	assert.EqualValues(t, 2, m.SchemaVersion)
	assert.Equal(t, types.OCIManifestSchema1, m.MediaType)

	// Config is the weight config descriptor.
	assert.Equal(t, types.MediaType(MediaTypeWeightConfig), m.Config.MediaType)
	assert.Equal(t, "sha256", m.Config.Digest.Algorithm)
	assert.Greater(t, m.Config.Size, int64(0))

	// Config blob is valid JSON containing the expected fields.
	cfgBytes, err := img.RawConfigFile()
	require.NoError(t, err)

	// Verify config digest matches the config blob bytes.
	cfgSum := sha256.Sum256(cfgBytes)
	assert.Equal(t, hex.EncodeToString(cfgSum[:]), m.Config.Digest.Hex)
	assert.Equal(t, int64(len(cfgBytes)), m.Config.Size)

	// Layers preserve media type, size, and digest from the packer, and
	// carry the uncompressed size annotation per spec §2.5.
	require.Len(t, m.Layers, len(layers))
	for i, layer := range m.Layers {
		assert.Equal(t, layers[i].MediaType, layer.MediaType)
		assert.Equal(t, layers[i].Size, layer.Size)
		assert.Equal(t, layers[i].Digest, layer.Digest)
		assert.Equal(t,
			strconv.FormatInt(layers[i].UncompressedSize, 10),
			layer.Annotations[AnnotationV1WeightSizeUncomp],
			"layer %d should carry uncompressed size annotation", i)
	}

	// Manifest annotations carry the v1 spec keys.
	assert.Equal(t, "z-image-turbo", m.Annotations[AnnotationV1WeightName])
	assert.Equal(t, "/src/weights", m.Annotations[AnnotationV1WeightTarget])
	assert.Equal(t, "sha256:0000000000000000000000000000000000000000000000000000000000000000", m.Annotations[AnnotationV1WeightSetDigest])
}

func TestBuildWeightManifestV1_RawManifestContainsArtifactType(t *testing.T) {
	layers := singleSmallFileLayers(t)
	entry := defaultEntry()
	img, err := buildWeightManifestV1(entry, layers)
	require.NoError(t, err)

	raw, err := img.RawManifest()
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed))

	assert.Equal(t, MediaTypeWeightArtifact, parsed["artifactType"])
	assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", parsed["mediaType"])
	assert.EqualValues(t, 2, parsed["schemaVersion"])

	cfg, ok := parsed["config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, MediaTypeWeightConfig, cfg["mediaType"])

	// Verify config digest matches what buildWeightManifestV1 produced.
	cfgBytes, err := img.RawConfigFile()
	require.NoError(t, err)
	cfgSum := sha256.Sum256(cfgBytes)
	assert.Equal(t, "sha256:"+hex.EncodeToString(cfgSum[:]), cfg["digest"])
	assert.EqualValues(t, len(cfgBytes), cfg["size"])

	rawLayers, ok := parsed["layers"].([]any)
	require.True(t, ok)
	require.Len(t, rawLayers, len(layers))
}

func TestBuildWeightManifestV1_DigestMatchesRawManifest(t *testing.T) {
	layers := singleSmallFileLayers(t)
	img, err := buildWeightManifestV1(defaultEntry(), layers)
	require.NoError(t, err)

	raw, err := img.RawManifest()
	require.NoError(t, err)

	sum := sha256.Sum256(raw)
	wantHex := hex.EncodeToString(sum[:])

	got, err := img.Digest()
	require.NoError(t, err)
	assert.Equal(t, wantHex, got.Hex)
	assert.Equal(t, "sha256", got.Algorithm)
}

func TestBuildWeightManifestV1_LayersCanonicallySortedByDigest(t *testing.T) {
	// The manifest emits layers in digest-sorted order regardless of input
	// order, so that different paths producing the same layer set produce
	// identical manifests. Mix a small bundle and two "large" files with
	// different media types to make sure the sort is by digest, not by
	// media type or size — and feed the builder the reverse of the
	// already-sorted order so a no-op pass-through can't spuriously pass.
	//
	// BundleFileMax is set tiny so ~1 KB files qualify as "large" and get
	// their own layer; avoids writing hundreds of MB per test.
	dir := t.TempDir()
	writeSrcFile(t, dir, "config.json", 64)
	writeSrcFile(t, dir, "tokenizer.json", 64)
	writeSrcFile(t, dir, "model.safetensors", 1024) // incompressible .tar
	writeSrcFile(t, dir, "aux.dat", 1024)           // compressible .tar.gz

	layers := packDir(t, dir, &packOptions{BundleFileMax: 512, BundleSizeMax: 1024})
	require.GreaterOrEqual(t, len(layers), 3, "expected bundle + 2 large layers")

	// Pre-sort then reverse so the input is guaranteed to be in the
	// opposite of the expected output order. If the builder forgets to
	// sort, the assertion below will fail.
	input := slices.Clone(layers)
	slices.SortFunc(input, func(a, b packedLayer) int {
		return strings.Compare(a.Digest.String(), b.Digest.String())
	})
	slices.Reverse(input)

	img, err := buildWeightManifestV1(defaultEntry(), input)
	require.NoError(t, err)

	m, err := img.Manifest()
	require.NoError(t, err)
	require.Len(t, m.Layers, len(layers))

	// Layers are digest-sorted; assert strict ascending order on the
	// serialized digest string.
	for i := 1; i < len(m.Layers); i++ {
		assert.Less(t, m.Layers[i-1].Digest.String(), m.Layers[i].Digest.String(),
			"layer %d digest should sort before layer %d (manifest must be digest-sorted)", i-1, i)
	}

	// At least one .tar and one .tar+gzip layer should be present — a
	// sanity check that the mixed media types didn't collapse.
	var sawTar, sawGzip bool
	for _, layer := range m.Layers {
		switch layer.MediaType {
		case types.MediaType(mediaTypeOCILayerTar):
			sawTar = true
		case types.MediaType(mediaTypeOCILayerTarGzip):
			sawGzip = true
		}
	}
	assert.True(t, sawTar, "expected at least one .tar layer")
	assert.True(t, sawGzip, "expected at least one .tar+gzip layer")
}

func TestBuildWeightManifestV1_InputOrderDoesNotAffectDigest(t *testing.T) {
	// Manifest digest must be a pure function of the layer set plus
	// metadata — permuting the input slice must not change the digest.
	dir := t.TempDir()
	writeSrcFile(t, dir, "config.json", 64)
	writeSrcFile(t, dir, "model.safetensors", 1024)
	writeSrcFile(t, dir, "aux.dat", 1024)

	layers := packDir(t, dir, &packOptions{BundleFileMax: 512, BundleSizeMax: 1024})
	require.GreaterOrEqual(t, len(layers), 3, "expected bundle + 2 large layers for a meaningful permutation test")

	imgOriginal, err := buildWeightManifestV1(defaultEntry(), layers)
	require.NoError(t, err)
	originalDigest, err := imgOriginal.Digest()
	require.NoError(t, err)

	// Reverse order.
	reversed := make([]packedLayer, len(layers))
	for i, l := range layers {
		reversed[len(layers)-1-i] = l
	}
	imgReversed, err := buildWeightManifestV1(defaultEntry(), reversed)
	require.NoError(t, err)
	reversedDigest, err := imgReversed.Digest()
	require.NoError(t, err)

	assert.Equal(t, originalDigest, reversedDigest, "manifest digest must be order-invariant")

	// Swap two adjacent layers.
	swapped := slices.Clone(layers)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	imgSwapped, err := buildWeightManifestV1(defaultEntry(), swapped)
	require.NoError(t, err)
	swappedDigest, err := imgSwapped.Digest()
	require.NoError(t, err)
	assert.Equal(t, originalDigest, swappedDigest, "manifest digest must be invariant under adjacent swap")
}

func TestBuildWeightManifestV1_DoesNotMutateInputSlice(t *testing.T) {
	// Callers keep the packer's or lockfile's layer order; the manifest
	// builder copies before sorting so that side effect is invisible.
	dir := t.TempDir()
	writeSrcFile(t, dir, "config.json", 64)
	writeSrcFile(t, dir, "model.safetensors", 1024)
	writeSrcFile(t, dir, "aux.dat", 1024)

	layers := packDir(t, dir, &packOptions{BundleFileMax: 512, BundleSizeMax: 1024})
	require.GreaterOrEqual(t, len(layers), 2, "need at least two layers to detect mutation")

	before := slices.Clone(layers)
	_, err := buildWeightManifestV1(defaultEntry(), layers)
	require.NoError(t, err)

	assert.Equal(t, before, layers, "buildWeightManifestV1 must not reorder the caller's slice")
}

func TestBuildWeightManifestV1_LayerDescriptorUncompressedSizeAnnotation(t *testing.T) {
	// Spec §2.5: each layer descriptor carries
	// run.cog.weight.size.uncompressed as the only layer-level
	// annotation. All other file-level metadata lives in the config
	// blob.
	layers := singleSmallFileLayers(t)
	img, err := buildWeightManifestV1(defaultEntry(), layers)
	require.NoError(t, err)

	m, err := img.Manifest()
	require.NoError(t, err)
	require.Len(t, m.Layers, len(layers))
	for i, l := range m.Layers {
		require.Len(t, l.Annotations, 1,
			"layer %d should carry exactly one annotation (uncompressed size)", i)
		assert.Equal(t,
			strconv.FormatInt(layers[i].UncompressedSize, 10),
			l.Annotations[AnnotationV1WeightSizeUncomp],
			"layer %d uncompressed size annotation", i)
	}
}

// =============================================================================
// fileLayer — interface contract
// =============================================================================

func TestFileLayer_ReturnsFileBytes(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "layer.tar")
	content := []byte("tar contents for fileLayer test")
	require.NoError(t, os.WriteFile(tmp, content, 0o644))

	sum := sha256.Sum256(content)
	lr := packedLayer{
		TarPath: tmp,
		Digest: v1.Hash{
			Algorithm: "sha256",
			Hex:       hex.EncodeToString(sum[:]),
		},
		Size:      int64(len(content)),
		MediaType: mediaTypeOCILayerTar,
	}

	l := newFileLayer(lr)

	d, err := l.Digest()
	require.NoError(t, err)
	assert.Equal(t, lr.Digest, d)

	diffID, err := l.DiffID()
	require.NoError(t, err)
	assert.Equal(t, d, diffID)

	sz, err := l.Size()
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), sz)

	mt, err := l.MediaType()
	require.NoError(t, err)
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTar), mt)

	// Compressed and Uncompressed both yield the raw file bytes (no re-encoding).
	for _, name := range []string{"Compressed", "Uncompressed"} {
		t.Run(name, func(t *testing.T) {
			var rc io.ReadCloser
			var err error
			if name == "Compressed" {
				rc, err = l.Compressed()
			} else {
				rc, err = l.Uncompressed()
			}
			require.NoError(t, err)
			defer rc.Close() //nolint:errcheck
			got, err := io.ReadAll(rc)
			require.NoError(t, err)
			assert.Equal(t, content, got)
		})
	}
}

func TestFileLayer_OpenMissingFile(t *testing.T) {
	lr := packedLayer{
		TarPath:   filepath.Join(t.TempDir(), "does-not-exist.tar"),
		Digest:    v1.Hash{Algorithm: "sha256", Hex: "deadbeef"},
		Size:      1,
		MediaType: mediaTypeOCILayerTar,
	}
	l := newFileLayer(lr)

	_, err := l.Compressed()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open layer file")
}
