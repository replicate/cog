package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
func packDir(t *testing.T, sourceDir string, opts *PackOptions) []LayerResult {
	t.Helper()
	src, err := weightsource.NewFileSource("file://"+sourceDir, "")
	require.NoError(t, err)
	inv, err := src.Inventory(t.Context())
	require.NoError(t, err)
	results, err := NewPacker(opts).Pack(t.Context(), src, inv)
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

// defaultMeta returns a minimal valid manifest metadata.
func defaultMeta() WeightManifestV1Metadata {
	return WeightManifestV1Metadata{
		Name:       "z-image-turbo",
		Target:     "/src/weights",
		SetDigest:  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		ConfigBlob: []byte(`{"name":"z-image-turbo","target":"/src/weights","setDigest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","files":[]}`),
	}
}

// singleSmallFileLayers produces a valid single-layer result set for tests that
// only care about manifest shape, not layer contents.
func singleSmallFileLayers(t *testing.T) []LayerResult {
	t.Helper()
	dir := t.TempDir()
	writeSrcFile(t, dir, "config.json", 128)
	return packDir(t, dir, nil)
}

// =============================================================================
// Metadata validation
// =============================================================================

func TestWeightManifestV1Metadata_validate(t *testing.T) {
	validSetDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	validConfigBlob := []byte(`{"name":"n","target":"/x","setDigest":"sha256:0000","files":[]}`)

	tests := []struct {
		name    string
		meta    WeightManifestV1Metadata
		wantErr string
	}{
		{"missing name", WeightManifestV1Metadata{Target: "/x", SetDigest: validSetDigest, ConfigBlob: validConfigBlob}, "weight name is required"},
		{"missing target", WeightManifestV1Metadata{Name: "n", SetDigest: validSetDigest, ConfigBlob: validConfigBlob}, "weight target is required"},
		{"missing set digest", WeightManifestV1Metadata{Name: "n", Target: "/x", ConfigBlob: validConfigBlob}, "weight set digest is required"},
		{"missing config blob", WeightManifestV1Metadata{Name: "n", Target: "/x", SetDigest: validSetDigest}, "weight config blob is required"},
		{"valid", WeightManifestV1Metadata{Name: "n", Target: "/x", SetDigest: validSetDigest, ConfigBlob: validConfigBlob}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.meta.validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestWeightManifestV1Metadata_annotations(t *testing.T) {
	meta := defaultMeta()
	anns := meta.annotations()

	assert.Equal(t, "z-image-turbo", anns[AnnotationV1WeightName])
	assert.Equal(t, "/src/weights", anns[AnnotationV1WeightTarget])
	assert.Equal(t, "sha256:0000000000000000000000000000000000000000000000000000000000000000", anns[AnnotationV1WeightSetDigest])

	// Removed annotations should not be present.
	_, hasRefType := anns[AnnotationV1ReferenceType]
	assert.False(t, hasRefType, "reference type annotation should not be present")
	_, hasRefDigest := anns[AnnotationV1ReferenceDigest]
	assert.False(t, hasRefDigest, "reference digest annotation should not be present")
}

// =============================================================================
// BuildWeightManifestV1 — validation
// =============================================================================

func TestBuildWeightManifestV1_RejectsMissingMetadata(t *testing.T) {
	layers := singleSmallFileLayers(t)

	_, err := BuildWeightManifestV1(layers, WeightManifestV1Metadata{Target: "/x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestBuildWeightManifestV1_RejectsEmptyLayers(t *testing.T) {
	_, err := BuildWeightManifestV1(nil, defaultMeta())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one layer")
}

func TestBuildWeightManifestV1_RejectsInvalidLayer(t *testing.T) {
	base := singleSmallFileLayers(t)

	cases := []struct {
		name    string
		mutate  func(lr *LayerResult)
		wantErr string
	}{
		{"missing TarPath", func(lr *LayerResult) { lr.TarPath = "" }, "missing TarPath"},
		{"missing digest", func(lr *LayerResult) { lr.Digest = v1.Hash{} }, "missing digest"},
		{"zero size", func(lr *LayerResult) { lr.Size = 0 }, "invalid size"},
		{"missing media type", func(lr *LayerResult) { lr.MediaType = "" }, "missing media type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lr := base[0]
			tc.mutate(&lr)
			_, err := BuildWeightManifestV1([]LayerResult{lr}, defaultMeta())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// =============================================================================
// BuildWeightManifestV1 — manifest structure
// =============================================================================

func TestBuildWeightManifestV1_ManifestShape(t *testing.T) {
	layers := singleSmallFileLayers(t)
	meta := defaultMeta()
	img, err := BuildWeightManifestV1(layers, meta)
	require.NoError(t, err)

	// Manifest schema and media type.
	m, err := img.Manifest()
	require.NoError(t, err)
	assert.EqualValues(t, 2, m.SchemaVersion)
	assert.Equal(t, types.OCIManifestSchema1, m.MediaType)

	// Config is the weight config descriptor.
	assert.Equal(t, types.MediaType(MediaTypeWeightConfig), m.Config.MediaType)
	assert.Equal(t, int64(len(meta.ConfigBlob)), m.Config.Size)
	assert.Equal(t, "sha256", m.Config.Digest.Algorithm)

	// Verify config digest matches the config blob.
	cfgSum := sha256.Sum256(meta.ConfigBlob)
	assert.Equal(t, hex.EncodeToString(cfgSum[:]), m.Config.Digest.Hex)

	// Config blob is the serialized config JSON on the wire.
	cfg, err := img.RawConfigFile()
	require.NoError(t, err)
	assert.Equal(t, meta.ConfigBlob, cfg)

	// Layers preserve media type, size, and digest from the packer. They
	// carry no annotations per spec §2.5.
	require.Len(t, m.Layers, len(layers))
	for i, layer := range m.Layers {
		assert.Equal(t, layers[i].MediaType, layer.MediaType)
		assert.Equal(t, layers[i].Size, layer.Size)
		assert.Equal(t, layers[i].Digest, layer.Digest)
		assert.Empty(t, layer.Annotations, "layer %d should have no descriptor annotations", i)
	}

	// Manifest annotations carry the v1 spec keys.
	assert.Equal(t, "z-image-turbo", m.Annotations[AnnotationV1WeightName])
	assert.Equal(t, "/src/weights", m.Annotations[AnnotationV1WeightTarget])
	assert.Equal(t, "sha256:0000000000000000000000000000000000000000000000000000000000000000", m.Annotations[AnnotationV1WeightSetDigest])
}

func TestBuildWeightManifestV1_RawManifestContainsArtifactType(t *testing.T) {
	layers := singleSmallFileLayers(t)
	meta := defaultMeta()
	img, err := BuildWeightManifestV1(layers, meta)
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

	cfgSum := sha256.Sum256(meta.ConfigBlob)
	assert.Equal(t, "sha256:"+hex.EncodeToString(cfgSum[:]), cfg["digest"])
	assert.EqualValues(t, len(meta.ConfigBlob), cfg["size"])

	rawLayers, ok := parsed["layers"].([]any)
	require.True(t, ok)
	require.Len(t, rawLayers, len(layers))
}

func TestBuildWeightManifestV1_DigestMatchesRawManifest(t *testing.T) {
	layers := singleSmallFileLayers(t)
	img, err := BuildWeightManifestV1(layers, defaultMeta())
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

func TestBuildWeightManifestV1_MultiLayerPreservesOrder(t *testing.T) {
	// Mix a small bundle and two large files with different media types.
	dir := t.TempDir()
	writeSrcFile(t, dir, "config.json", 128)
	writeSrcFile(t, dir, "tokenizer.json", 64)
	writeSrcFile(t, dir, "model.safetensors", 100*1024*1024) // incompressible .tar
	writeSrcFile(t, dir, "aux.dat", 100*1024*1024)           // compressible .tar.gz

	layers := packDir(t, dir, nil)
	require.GreaterOrEqual(t, len(layers), 3, "expected bundle + 2 large layers")

	img, err := BuildWeightManifestV1(layers, defaultMeta())
	require.NoError(t, err)

	m, err := img.Manifest()
	require.NoError(t, err)
	require.Len(t, m.Layers, len(layers))

	// At least one .tar and one .tar+gzip layer should be present.
	var sawTar, sawGzip bool
	for i, layer := range m.Layers {
		assert.Equal(t, layers[i].MediaType, layer.MediaType)
		switch layer.MediaType {
		case types.MediaType(MediaTypeOCILayerTar):
			sawTar = true
		case types.MediaType(MediaTypeOCILayerTarGzip):
			sawGzip = true
		}
	}
	assert.True(t, sawTar, "expected at least one .tar layer")
	assert.True(t, sawGzip, "expected at least one .tar+gzip layer")
}

func TestBuildWeightManifestV1_LayerDescriptorsHaveNoAnnotations(t *testing.T) {
	// Spec §2.5: layer descriptors on weight manifests MUST NOT carry
	// annotations. Everything useful lives in the config blob or the
	// lockfile.
	layers := singleSmallFileLayers(t)
	img, err := BuildWeightManifestV1(layers, defaultMeta())
	require.NoError(t, err)

	m, err := img.Manifest()
	require.NoError(t, err)
	for i, l := range m.Layers {
		assert.Empty(t, l.Annotations, "layer %d should have no annotations", i)
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
	lr := LayerResult{
		TarPath: tmp,
		Digest: v1.Hash{
			Algorithm: "sha256",
			Hex:       hex.EncodeToString(sum[:]),
		},
		Size:      int64(len(content)),
		MediaType: MediaTypeOCILayerTar,
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
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTar), mt)

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
	lr := LayerResult{
		TarPath:   filepath.Join(t.TempDir(), "does-not-exist.tar"),
		Digest:    v1.Hash{Algorithm: "sha256", Hex: "deadbeef"},
		Size:      1,
		MediaType: MediaTypeOCILayerTar,
	}
	l := newFileLayer(lr)

	_, err := l.Compressed()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open layer file")
}
