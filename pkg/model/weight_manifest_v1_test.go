package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helpers
// =============================================================================

// packDir runs Pack on sourceDir and registers cleanup of the produced tar files.
func packDir(t *testing.T, sourceDir string, opts *PackOptions) []LayerResult {
	t.Helper()
	results, err := Pack(context.Background(), sourceDir, opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, r := range results {
			_ = os.Remove(r.TarPath)
		}
	})
	return results
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
		Name:            "z-image-turbo",
		Target:          "/src/weights",
		ReferenceDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		Created:         time.Date(2026, 4, 16, 17, 27, 7, 0, time.UTC),
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
	tests := []struct {
		name    string
		meta    WeightManifestV1Metadata
		wantErr string
	}{
		{"missing name", WeightManifestV1Metadata{Target: "/x"}, "weight name is required"},
		{"missing target", WeightManifestV1Metadata{Name: "n"}, "weight target is required"},
		{"valid", WeightManifestV1Metadata{Name: "n", Target: "/x"}, ""},
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
	assert.Equal(t, ReferenceTypeWeights, anns[AnnotationV1ReferenceType])
	assert.Equal(t, "sha256:1111111111111111111111111111111111111111111111111111111111111111", anns[AnnotationV1ReferenceDigest])
	assert.Equal(t, "2026-04-16T17:27:07Z", anns[AnnotationOCIImageCreated])
}

func TestWeightManifestV1Metadata_annotations_OmitsBlankReferenceDigest(t *testing.T) {
	meta := WeightManifestV1Metadata{Name: "n", Target: "/x"}
	anns := meta.annotations()

	_, present := anns[AnnotationV1ReferenceDigest]
	assert.False(t, present, "reference.digest annotation should be omitted when empty")
}

func TestWeightManifestV1Metadata_annotations_DefaultsCreatedToNow(t *testing.T) {
	meta := WeightManifestV1Metadata{Name: "n", Target: "/x"}
	before := time.Now().UTC().Add(-time.Second)
	anns := meta.annotations()
	after := time.Now().UTC().Add(time.Second)

	got, err := time.Parse(time.RFC3339, anns[AnnotationOCIImageCreated])
	require.NoError(t, err)
	assert.True(t, !got.Before(before) && !got.After(after),
		"created annotation %s should be in [%s, %s]", got, before, after)
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
	img, err := BuildWeightManifestV1(layers, defaultMeta())
	require.NoError(t, err)

	// Manifest schema and media type.
	m, err := img.Manifest()
	require.NoError(t, err)
	assert.EqualValues(t, 2, m.SchemaVersion)
	assert.Equal(t, types.OCIManifestSchema1, m.MediaType)

	// Config is the OCI empty descriptor.
	assert.Equal(t, types.MediaType(MediaTypeOCIEmpty), m.Config.MediaType)
	assert.Equal(t, int64(2), m.Config.Size)
	assert.Equal(t, emptyBlobSHA256, m.Config.Digest.Hex)
	assert.Equal(t, "sha256", m.Config.Digest.Algorithm)

	// Config blob is `{}` on the wire.
	cfg, err := img.RawConfigFile()
	require.NoError(t, err)
	assert.Equal(t, []byte(`{}`), cfg)

	// Layers preserve per-layer media type + annotations from the packer.
	require.Len(t, m.Layers, len(layers))
	for i, layer := range m.Layers {
		assert.Equal(t, layers[i].MediaType, layer.MediaType)
		assert.Equal(t, layers[i].Size, layer.Size)
		assert.Equal(t, layers[i].Digest, layer.Digest)
		for k, v := range layers[i].Annotations {
			assert.Equal(t, v, layer.Annotations[k], "layer %d annotation %s", i, k)
		}
	}

	// Manifest annotations carry the v1 spec keys.
	assert.Equal(t, "z-image-turbo", m.Annotations[AnnotationV1WeightName])
	assert.Equal(t, "/src/weights", m.Annotations[AnnotationV1WeightTarget])
	assert.Equal(t, ReferenceTypeWeights, m.Annotations[AnnotationV1ReferenceType])
	assert.Contains(t, m.Annotations[AnnotationV1ReferenceDigest], "sha256:")
	assert.Equal(t, "2026-04-16T17:27:07Z", m.Annotations[AnnotationOCIImageCreated])
}

func TestBuildWeightManifestV1_RawManifestContainsArtifactType(t *testing.T) {
	layers := singleSmallFileLayers(t)
	img, err := BuildWeightManifestV1(layers, defaultMeta())
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
	assert.Equal(t, MediaTypeOCIEmpty, cfg["mediaType"])
	assert.Equal(t, "sha256:"+emptyBlobSHA256, cfg["digest"])
	assert.EqualValues(t, 2, cfg["size"])

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

func TestBuildWeightManifestV1_AnnotationsAreClonedFromLayerResult(t *testing.T) {
	layers := singleSmallFileLayers(t)
	img, err := BuildWeightManifestV1(layers, defaultMeta())
	require.NoError(t, err)

	// Mutating the source layer's annotations after build must not affect the manifest.
	layers[0].Annotations["run.cog.weight.content"] = "tampered"

	m, err := img.Manifest()
	require.NoError(t, err)
	assert.Equal(t, ContentBundle, m.Layers[0].Annotations[AnnotationV1WeightContent])
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
