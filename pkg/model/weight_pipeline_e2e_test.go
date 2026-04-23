package model

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/registry_testhelpers"
)

// TestWeightPipeline_EndToEnd exercises Pack → WeightPusher against a
// real test registry, then pulls each layer back and asserts the
// extracted contents match the source directory byte-for-byte.
//
// This covers the critical property "does the v1 artifact extract to
// the correct shape on disk" that would otherwise require a human
// running crane + tar locally.
//
// The source dir is sized so all three packer branches fire under a
// small bundle threshold (1 KiB):
//   - a bundle layer (tar+gzip) for the small config/tokenizer files
//   - an uncompressed tar layer for a single .safetensors (incompressible)
//   - a gzipped tar layer for a single .nemo (compressible)
func TestWeightPipeline_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := t.Context()
	reg := registry_testhelpers.StartTestRegistry(t)
	regHost := reg.RegistryHost()

	// Deterministic content so assertions compare the pushed bytes
	// against what's on disk without depending on randomness. The
	// safetensors and .nemo files exceed the 1 KiB BundleFileMax we
	// pass to Pack, forcing them into single-file layers.
	sources := map[string][]byte{
		"config.json":            []byte(`{"hidden_size": 768}`),
		"tokenizer.json":         []byte(`{"vocab_size": 50257}`),
		"generation_config.json": []byte(`{"max_length": 128}`),
		"plots/asr.png":          []byte("PNG\x89fake-png-bytes-for-deterministic-hash"),
		"model.safetensors":      bytes.Repeat([]byte{'S'}, 4096),
		"model.nemo":             bytes.Repeat([]byte{'N'}, 4096),
	}

	sourceDir := t.TempDir()
	writeSourceTree(t, sourceDir, sources)

	// Pack directly with a small bundle threshold so we don't need to
	// write 64+ MiB of fixture content to cross the default cutoff.
	packDir := t.TempDir()
	src, err := weightsource.NewFileSource("file://"+sourceDir, "")
	require.NoError(t, err, "source")
	inv, err := src.Inventory(ctx)
	require.NoError(t, err, "inventory")
	pr, err := newPacker(&packOptions{
		BundleFileMax: 1024,
		TempDir:       packDir,
	}).pack(ctx, src, inv)
	require.NoError(t, err, "pack")
	require.Len(t, pr.Layers, 3, "want 1 bundle + 2 single-file layers")

	// Build a lock entry and artifact (manifest + descriptor + digest backfill).
	entry := newWeightLockEntry("my-model", "/src/weights", WeightLockSource{}, pr.Files, pr.Layers)
	artifact, err := buildWeightArtifact(&entry, pr.Layers)
	require.NoError(t, err)
	setDigest := entry.SetDigest

	repo := regHost + "/test/my-model"
	pusher := NewWeightPusher(registry.NewRegistryClient())
	result, err := pusher.Push(ctx, repo, artifact)
	require.NoError(t, err, "push weights")
	require.NotEmpty(t, result.Ref, "push result missing ref")

	// Pull the manifest back and assert spec-compliant shape.
	manifestRef, err := name.ParseReference(result.Ref, name.Insecure)
	require.NoError(t, err)
	pulled, err := remote.Image(manifestRef)
	require.NoError(t, err)

	mf, err := pulled.Manifest()
	require.NoError(t, err)

	// go-containerregistry's v1.Manifest omits artifactType, so parse
	// the raw manifest bytes to verify it.
	rawManifest, err := pulled.RawManifest()
	require.NoError(t, err)
	var rawMf struct {
		ArtifactType string `json:"artifactType"`
	}
	require.NoError(t, json.Unmarshal(rawManifest, &rawMf))
	assert.Equal(t, "application/vnd.cog.weight.v1", rawMf.ArtifactType)

	assert.Equal(t, "my-model", mf.Annotations[AnnotationV1WeightName])
	assert.Equal(t, "/src/weights", mf.Annotations[AnnotationV1WeightTarget])
	assert.Equal(t, setDigest, mf.Annotations[AnnotationV1WeightSetDigest])

	require.Len(t, mf.Layers, 3)

	// Layer descriptors carry no annotations (spec §2.5); partition by
	// media type + extracted contents instead. The single uncompressed
	// .tar layer is model.safetensors; the single .tar+gzip large-file
	// layer is model.nemo; the remaining .tar+gzip layer is the bundle
	// containing the JSON/PNG files.
	var bundleCount int
	var safetensorsLayer, nemoLayer *v1.Descriptor
	for i := range mf.Layers {
		d := &mf.Layers[i]
		assert.Empty(t, d.Annotations, "layer descriptors must carry no annotations per spec §2.5")

		paths := listFilesInPushedLayer(t, repo+"@"+d.Digest.String(), string(d.MediaType))
		switch {
		case len(paths) > 1:
			bundleCount++
		case len(paths) == 1 && paths[0] == "model.safetensors":
			safetensorsLayer = d
		case len(paths) == 1 && paths[0] == "model.nemo":
			nemoLayer = d
		default:
			t.Fatalf("layer %s has unexpected file set %v", d.Digest, paths)
		}
	}
	assert.Equal(t, 1, bundleCount, "expected exactly one bundle layer")
	require.NotNil(t, safetensorsLayer)
	require.NotNil(t, nemoLayer)

	// .safetensors stays uncompressed per spec §1.2; .nemo gets gzipped.
	assert.Equal(t, mediaTypeOCILayerTar, string(safetensorsLayer.MediaType),
		"model.safetensors should be uncompressed tar")
	assert.Equal(t, mediaTypeOCILayerTarGzip, string(nemoLayer.MediaType),
		"model.nemo should be gzipped")

	// Pull each layer, extract it, and assert the extracted tree
	// matches the source byte-for-byte.
	extractDir := t.TempDir()
	for _, l := range mf.Layers {
		blobRef := repo + "@" + l.Digest.String()
		extractLayerToDir(t, blobRef, string(l.MediaType), extractDir)
	}

	for relPath, want := range sources {
		gotPath := filepath.Join(extractDir, relPath)
		got, err := os.ReadFile(gotPath) //nolint:gosec // G304: relPath is a test constant
		require.NoError(t, err, "extracted file %q missing under %s", relPath, extractDir)
		assert.Equal(t, sha256Hex(want), sha256Hex(got),
			"content mismatch for extracted file %q", relPath)
	}
}

// writeSourceTree materializes a map of relative-path → content into
// a directory, creating parent directories on demand.
func writeSourceTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for relPath, data := range files {
		full := filepath.Join(dir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, data, 0o644))
	}
}

// extractLayerToDir pulls a layer blob and extracts regular files into
// destDir, preserving relative paths.
func extractLayerToDir(t *testing.T, blobRef, mediaType, destDir string) {
	t.Helper()

	rc := openLayerStream(t, blobRef, mediaType)
	defer rc.Close() //nolint:errcheck

	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return
		}
		require.NoError(t, err, "read tar header")

		target := filepath.Join(destDir, hdr.Name) //nolint:gosec // G305: test-only, author-controlled tar input
		switch hdr.Typeflag {
		case tar.TypeDir:
			require.NoError(t, os.MkdirAll(target, 0o755))
		case tar.TypeReg:
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
			f, err := os.Create(target) //nolint:gosec // G304: test-only
			require.NoError(t, err)
			_, err = io.Copy(f, tr) //nolint:gosec // G110: test-only with small bounded inputs
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}
	}
}

// listFilesInPushedLayer pulls a layer blob and returns the paths of
// the regular files it contains. Used to classify layers by content
// now that layer descriptors carry no content annotations.
func listFilesInPushedLayer(t *testing.T, blobRef, mediaType string) []string {
	t.Helper()

	rc := openLayerStream(t, blobRef, mediaType)
	defer rc.Close() //nolint:errcheck

	var paths []string
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return paths
		}
		require.NoError(t, err)
		if hdr.Typeflag == tar.TypeReg {
			paths = append(paths, hdr.Name)
		}
	}
}

// openLayerStream pulls a layer blob by digest and returns a reader
// for its uncompressed tar bytes.
func openLayerStream(t *testing.T, blobRef, mediaType string) io.ReadCloser {
	t.Helper()

	ref, err := name.ParseReference(blobRef, name.Insecure)
	require.NoError(t, err)

	digest, ok := ref.(name.Digest)
	require.True(t, ok, "expected digest reference, got %T", ref)

	layer, err := remote.Layer(digest)
	require.NoError(t, err)

	raw, err := layer.Compressed()
	require.NoError(t, err)

	if mediaType == mediaTypeOCILayerTarGzip {
		gr, err := gzip.NewReader(raw)
		require.NoError(t, err)
		return &gzipReadCloser{Reader: gr, underlying: raw}
	}
	return raw
}

// gzipReadCloser composes a *gzip.Reader with the underlying HTTP body
// so Close closes both.
type gzipReadCloser struct {
	*gzip.Reader
	underlying io.Closer
}

func (g *gzipReadCloser) Close() error {
	_ = g.Reader.Close()
	return g.underlying.Close()
}

// sha256Hex hashes b as a hex string, convenient for assertion output.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
