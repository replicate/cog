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
	"strconv"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	layers, err := Pack(ctx, sourceDir, &PackOptions{
		BundleFileMax: 1024,
		TempDir:       packDir,
	})
	require.NoError(t, err, "pack")
	require.Len(t, layers, 3, "want 1 bundle + 2 single-file layers")

	// Build the manifest and wrap it as an artifact the pusher accepts.
	// Pinned Created so the manifest is deterministic across runs.
	created := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	img, err := BuildWeightManifestV1(layers, WeightManifestV1Metadata{
		Name:    "my-model",
		Target:  "/src/weights",
		Created: created,
	})
	require.NoError(t, err)
	desc, err := descriptorFromImage(img)
	require.NoError(t, err)

	artifact := NewWeightArtifact("my-model", desc, "/src/weights", layers)
	artifact.Created = created

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
	assert.Equal(t, ReferenceTypeWeights, mf.Annotations[AnnotationV1ReferenceType])
	_, hasRefDigest := mf.Annotations[AnnotationV1ReferenceDigest]
	assert.False(t, hasRefDigest, "standalone push should not set reference.digest")

	require.Len(t, mf.Layers, 3)

	// Partition manifest layers by content annotation so assertions
	// don't depend on packer-emitted ordering.
	var (
		bundleLayer *v1.Descriptor
		fileLayers  = map[string]*v1.Descriptor{}
	)
	for i, l := range mf.Layers {
		d := &mf.Layers[i]
		switch l.Annotations[AnnotationV1WeightContent] {
		case ContentBundle:
			require.Nil(t, bundleLayer, "manifest has multiple bundle layers")
			bundleLayer = d
		case ContentFile:
			fname := l.Annotations[AnnotationV1WeightFile]
			require.NotEmpty(t, fname, "file layer missing run.cog.weight.file annotation")
			fileLayers[fname] = d
		default:
			t.Fatalf("layer %d has unknown content annotation %q", i, l.Annotations[AnnotationV1WeightContent])
		}
	}
	require.NotNil(t, bundleLayer, "manifest missing bundle layer")
	require.Contains(t, fileLayers, "model.safetensors")
	require.Contains(t, fileLayers, "model.nemo")

	// .safetensors stays uncompressed per spec §1.2; .nemo gets gzipped.
	assert.Equal(t, MediaTypeOCILayerTar, string(fileLayers["model.safetensors"].MediaType),
		"model.safetensors should be uncompressed tar")
	assert.Equal(t, MediaTypeOCILayerTarGzip, string(fileLayers["model.nemo"].MediaType),
		"model.nemo should be gzipped")

	// Pull each layer, extract it, and assert the extracted tree
	// matches the source byte-for-byte.
	extractDir := t.TempDir()
	for _, l := range mf.Layers {
		blobRef := repo + "@" + l.Digest.String()
		extractLayerToDir(t, blobRef, string(l.MediaType), extractDir)

		// run.cog.weight.size.uncompressed is defined as the sum of
		// regular-file bytes in the layer (tar header overhead is
		// excluded per packer.go). Verify.
		want, err := strconv.ParseInt(l.Annotations[AnnotationV1WeightSizeUncomp], 10, 64)
		require.NoError(t, err, "parse run.cog.weight.size.uncompressed on %s", l.Digest)
		got := sumFileBytesInLayer(t, blobRef, string(l.MediaType))
		assert.Equal(t, want, got,
			"size.uncompressed annotation (%d) doesn't match extracted file bytes (%d) for %s",
			want, got, l.Digest)
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

// sumFileBytesInLayer reads a layer tar and returns the sum of regular
// file sizes, matching the semantics of run.cog.weight.size.uncompressed.
func sumFileBytesInLayer(t *testing.T, blobRef, mediaType string) int64 {
	t.Helper()

	rc := openLayerStream(t, blobRef, mediaType)
	defer rc.Close() //nolint:errcheck

	var total int64
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return total
		}
		require.NoError(t, err)
		if hdr.Typeflag == tar.TypeReg {
			total += hdr.Size
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

	if mediaType == MediaTypeOCILayerTarGzip {
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
