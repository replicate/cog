package model

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestFile creates a file at the given path (relative to dir) with the given size.
func createTestFile(t *testing.T, dir, relPath string, size int64) {
	t.Helper()
	absPath := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
	f, err := os.Create(absPath)
	require.NoError(t, err)
	defer f.Close()
	if size > 0 {
		require.NoError(t, f.Truncate(size))
	}
}

func TestPack_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := Pack(context.Background(), dir, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files found")
}

func TestPack_SingleSmallFile(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "config.json", 100)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), r.MediaType)
	assert.Equal(t, ContentBundle, r.Annotations[AnnotationV1WeightContent])
	assert.Equal(t, "100", r.Annotations[AnnotationV1WeightSizeUncomp])
	assert.Empty(t, r.Annotations[AnnotationV1WeightFile]) // bundles don't have file annotation
	assert.True(t, r.Size > 0)
	assert.Equal(t, int64(100), r.UncompressedSize)
	assert.NotEmpty(t, r.Digest.Hex)
	assert.Equal(t, "sha256", r.Digest.Algorithm)

	// Verify tar contents.
	entries := readTarGzEntries(t, r.TarPath)
	require.Len(t, entries, 1)
	assert.Equal(t, "config.json", entries[0])
}

func TestPack_SingleLargeFile_Incompressible(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "model.safetensors", 100*1024*1024) // 100 MB

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTar), r.MediaType)
	assert.Equal(t, ContentFile, r.Annotations[AnnotationV1WeightContent])
	assert.Equal(t, "model.safetensors", r.Annotations[AnnotationV1WeightFile])
	assert.Equal(t, int64(100*1024*1024), r.UncompressedSize)
}

func TestPack_SingleLargeFile_Compressible(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "model.dat", 100*1024*1024) // 100 MB, not in skip set

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), r.MediaType)
	assert.Equal(t, ContentFile, r.Annotations[AnnotationV1WeightContent])
	assert.Equal(t, "model.dat", r.Annotations[AnnotationV1WeightFile])
}

func TestPack_MixedFiles(t *testing.T) {
	dir := t.TempDir()

	// Small files (< 64 MB default threshold).
	createTestFile(t, dir, "config.json", 500)
	createTestFile(t, dir, "tokenizer.json", 1000)
	createTestFile(t, dir, "special_tokens_map.json", 200)

	// Large files.
	createTestFile(t, dir, "model-00001.safetensors", 100*1024*1024)
	createTestFile(t, dir, "model-00002.safetensors", 100*1024*1024)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 3) // 1 bundle + 2 large files

	// First result should be the bundle (small files come first in output).
	bundle := results.Layers[0]
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), bundle.MediaType)
	assert.Equal(t, ContentBundle, bundle.Annotations[AnnotationV1WeightContent])

	bundleEntries := readTarGzEntries(t, bundle.TarPath)
	// Files should be sorted by path.
	assert.Equal(t, []string{"config.json", "special_tokens_map.json", "tokenizer.json"}, bundleEntries)

	// Large files should be uncompressed tars (safetensors is incompressible).
	for _, r := range results.Layers[1:] {
		assert.Equal(t, types.MediaType(MediaTypeOCILayerTar), r.MediaType)
		assert.Equal(t, ContentFile, r.Annotations[AnnotationV1WeightContent])
	}
}

func TestPack_NestedDirectories(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "text_encoder/config.json", 100)
	createTestFile(t, dir, "text_encoder/tokenizer.json", 200)
	createTestFile(t, dir, "vae/config.json", 150)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1) // All small, one bundle.

	entries := readTarGzEntries(t, results.Layers[0].TarPath)
	// Directories come first (sorted), then files (sorted).
	expected := []string{
		"text_encoder/",
		"vae/",
		"text_encoder/config.json",
		"text_encoder/tokenizer.json",
		"vae/config.json",
	}
	assert.Equal(t, expected, entries)
}

func TestPack_LargeFileInSubdir(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "text_encoder/model-00001.safetensors", 100*1024*1024)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, "text_encoder/model-00001.safetensors", r.Annotations[AnnotationV1WeightFile])

	entries := readTarEntries(t, r.TarPath)
	expected := []string{
		"text_encoder/",
		"text_encoder/model-00001.safetensors",
	}
	assert.Equal(t, expected, entries)
}

func TestPack_BundleSizeMaxSplits(t *testing.T) {
	dir := t.TempDir()

	// Create 3 files of 10 bytes each. Set bundle max to 20 so it splits.
	createTestFile(t, dir, "a.txt", 10)
	createTestFile(t, dir, "b.txt", 10)
	createTestFile(t, dir, "c.txt", 10)

	opts := &PackOptions{
		BundleFileMax: 1024, // Everything is "small".
		BundleSizeMax: 20,   // Forces split: a+b in one bundle, c in another.
	}

	results, err := Pack(context.Background(), dir, opts)
	require.NoError(t, err)
	require.Len(t, results.Layers, 2)

	// Both should be gzipped bundles.
	for _, r := range results.Layers {
		assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), r.MediaType)
		assert.Equal(t, ContentBundle, r.Annotations[AnnotationV1WeightContent])
	}

	// First bundle should have a.txt and b.txt.
	entries1 := readTarGzEntries(t, results.Layers[0].TarPath)
	assert.Equal(t, []string{"a.txt", "b.txt"}, entries1)

	// Second bundle should have c.txt.
	entries2 := readTarGzEntries(t, results.Layers[1].TarPath)
	assert.Equal(t, []string{"c.txt"}, entries2)
}

func TestPack_CustomThresholds(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "small.txt", 50)
	createTestFile(t, dir, "large.bin", 200)

	opts := &PackOptions{
		BundleFileMax: 100, // 50 is small, 200 is large
	}

	results, err := Pack(context.Background(), dir, opts)
	require.NoError(t, err)
	require.Len(t, results.Layers, 2)

	// Bundle for small file.
	assert.Equal(t, ContentBundle, results.Layers[0].Annotations[AnnotationV1WeightContent])
	// Individual layer for large file (.bin is in incompressible set, so uncompressed).
	assert.Equal(t, ContentFile, results.Layers[1].Annotations[AnnotationV1WeightContent])
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTar), results.Layers[1].MediaType)
}

func TestPack_SkipsDotCogDirectory(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "config.json", 100)
	createTestFile(t, dir, ".cog/manifest.json", 50)
	createTestFile(t, dir, ".cog/ready", 0)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	entries := readTarGzEntries(t, results.Layers[0].TarPath)
	assert.Equal(t, []string{"config.json"}, entries)
}

func TestPack_DeterministicTarProperties(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "data.txt", 100)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	// Read the tar and inspect headers.
	f, err := os.Open(results.Layers[0].TarPath)
	require.NoError(t, err)
	defer f.Close()

	gr, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer gr.Close()

	epoch := time.Unix(0, 0)

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		assert.Equal(t, epoch, hdr.ModTime, "mtime should be Unix epoch")
		assert.Equal(t, 0, hdr.Uid, "uid should be 0")
		assert.Equal(t, 0, hdr.Gid, "gid should be 0")

		switch hdr.Typeflag {
		case tar.TypeReg:
			assert.Equal(t, int64(0o644), hdr.Mode, "file mode should be 0644")
		case tar.TypeDir:
			assert.Equal(t, int64(0o755), hdr.Mode, "dir mode should be 0755")
		}
	}
}

func TestPack_DigestDeterminism(t *testing.T) {
	// Pack the same directory twice and verify digests match.
	dir := t.TempDir()
	createTestFile(t, dir, "a.txt", 100)
	createTestFile(t, dir, "b.txt", 200)

	results1, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)

	results2, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)

	require.Len(t, results1.Layers, len(results2.Layers))
	for i := range results1.Layers {
		assert.Equal(t, results1.Layers[i].Digest, results2.Layers[i].Digest,
			"digest mismatch for result %d", i)
		assert.Equal(t, results1.Layers[i].Size, results2.Layers[i].Size,
			"size mismatch for result %d", i)
	}
}

func TestPack_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "file.txt", 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := Pack(ctx, dir, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPack_IncompressibleExtensions(t *testing.T) {
	tests := []struct {
		ext       string
		mediaType types.MediaType
	}{
		{".safetensors", MediaTypeOCILayerTar},
		{".bin", MediaTypeOCILayerTar},
		{".gguf", MediaTypeOCILayerTar},
		{".onnx", MediaTypeOCILayerTar},
		{".parquet", MediaTypeOCILayerTar},
		{".pt", MediaTypeOCILayerTar},
		{".pth", MediaTypeOCILayerTar},
		{".dat", MediaTypeOCILayerTarGzip},   // compressible
		{".json", MediaTypeOCILayerTarGzip},   // compressible
		{".pickle", MediaTypeOCILayerTarGzip}, // compressible
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			dir := t.TempDir()
			createTestFile(t, dir, "model"+tt.ext, 100*1024*1024)

			results, err := Pack(context.Background(), dir, nil)
			require.NoError(t, err)
			require.Len(t, results.Layers, 1)
			assert.Equal(t, tt.mediaType, results.Layers[0].MediaType)
		})
	}
}

func TestPack_FileAtExactThreshold(t *testing.T) {
	dir := t.TempDir()
	// File exactly at the threshold should be "large" (>= bundle_file_max).
	createTestFile(t, dir, "model.bin", DefaultBundleFileMax)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)
	assert.Equal(t, ContentFile, results.Layers[0].Annotations[AnnotationV1WeightContent])
}

func TestPack_FileJustBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	// File just below the threshold should be bundled.
	createTestFile(t, dir, "model.bin", DefaultBundleFileMax-1)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)
	assert.Equal(t, ContentBundle, results.Layers[0].Annotations[AnnotationV1WeightContent])
}

func TestPack_CleanupTarFiles(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "a.txt", 100)
	createTestFile(t, dir, "big.safetensors", 100*1024*1024)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)

	// Verify all tar files exist.
	for _, r := range results.Layers {
		_, err := os.Stat(r.TarPath)
		assert.NoError(t, err, "tar file should exist: %s", r.TarPath)
	}

	// Clean them up (as a caller would).
	for _, r := range results.Layers {
		require.NoError(t, os.Remove(r.TarPath))
	}
}

func TestCollectDirsForPath(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
	}{
		{"file.txt", nil},
		{"a/file.txt", []string{"a"}},
		{"a/b/file.txt", []string{"a", "a/b"}},
		{"a/b/c/file.txt", []string{"a", "a/b", "a/b/c"}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := collectDirsForPath(tt.path)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestCollectDirs(t *testing.T) {
	files := []fileEntry{
		{relPath: "b/c/file.txt"},
		{relPath: "a/file.txt"},
		{relPath: "b/file.txt"},
		{relPath: "root.txt"},
	}
	got := collectDirs(files)
	expected := []string{"a", "b", "b/c"}
	assert.Equal(t, expected, got, "dirs should be sorted and deduplicated")
}

// readTarGzEntries reads a .tar.gz file and returns entry names in tar order.
func readTarGzEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	gr, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer gr.Close()

	return readTarNames(t, tar.NewReader(gr))
}

// readTarEntries reads a .tar file and returns file names in order.
func readTarEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	return readTarNames(t, tar.NewReader(f))
}

// readTarNames reads all entry names from a tar reader.
func readTarNames(t *testing.T, tr *tar.Reader) []string {
	t.Helper()
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	return names
}

// Verify that sorting files produces stable, deterministic ordering.
func TestSmallFileSortingStability(t *testing.T) {
	files := []fileEntry{
		{relPath: "z.txt", size: 10},
		{relPath: "a.txt", size: 10},
		{relPath: "m/b.txt", size: 10},
		{relPath: "m/a.txt", size: 10},
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i].relPath < files[j].relPath
	})

	expected := []string{"a.txt", "m/a.txt", "m/b.txt", "z.txt"}
	var got []string
	for _, f := range files {
		got = append(got, f.relPath)
	}
	assert.Equal(t, expected, got)
}

func TestPack_DeepNestedDirsInLargeFile(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "a/b/c/model.safetensors", 100*1024*1024)

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	entries := readTarEntries(t, results.Layers[0].TarPath)
	expected := []string{
		"a/",
		"a/b/",
		"a/b/c/",
		"a/b/c/model.safetensors",
	}
	assert.Equal(t, expected, entries)
}

func TestPack_WorkedExample(t *testing.T) {
	// Simulate the z-image-turbo layout from spec §4.
	dir := t.TempDir()

	// Small files (configs, tokenizers) — all < 64 MB.
	smallFiles := []string{
		"config.json",
		"model_index.json",
		"tokenizer/tokenizer_config.json",
		"tokenizer/special_tokens_map.json",
		"tokenizer/vocab.json",
		"tokenizer/merges.txt",
	}
	for _, f := range smallFiles {
		createTestFile(t, dir, f, 1024) // 1 KB each
	}

	// Large files (safetensors) — each > 64 MB.
	largeFiles := []string{
		"text_encoder/model-00001-of-00003.safetensors",
		"text_encoder/model-00002-of-00003.safetensors",
		"text_encoder/model-00003-of-00003.safetensors",
		"vae/diffusion_pytorch_model.safetensors",
		"transformer/diffusion_pytorch_model-00001-of-00003.safetensors",
		"transformer/diffusion_pytorch_model-00002-of-00003.safetensors",
		"transformer/diffusion_pytorch_model-00003-of-00003.safetensors",
	}
	for _, f := range largeFiles {
		createTestFile(t, dir, f, 100*1024*1024)
	}

	results, err := Pack(context.Background(), dir, nil)
	require.NoError(t, err)

	// 1 bundle for small files + 7 individual layers for large files = 8 total.
	require.Len(t, results.Layers, 8)

	// First result is the bundle.
	assert.Equal(t, ContentBundle, results.Layers[0].Annotations[AnnotationV1WeightContent])
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), results.Layers[0].MediaType)

	// Remaining 7 are individual files.
	for i := 1; i <= 7; i++ {
		r := results.Layers[i]
		assert.Equal(t, ContentFile, r.Annotations[AnnotationV1WeightContent])
		assert.Equal(t, types.MediaType(MediaTypeOCILayerTar), r.MediaType)
		assert.NotEmpty(t, r.Annotations[AnnotationV1WeightFile])
		assert.True(t, strings.HasSuffix(r.Annotations[AnnotationV1WeightFile], ".safetensors"))
	}

	// Verify no path appears in more than one layer (order-independence).
	allPaths := make(map[string]int)
	for i, r := range results.Layers {
		var entries []string
		if r.MediaType == MediaTypeOCILayerTarGzip {
			entries = readTarGzEntries(t, r.TarPath)
		} else {
			entries = readTarEntries(t, r.TarPath)
		}
		for _, e := range entries {
			if strings.HasSuffix(e, "/") {
				continue // Skip directory entries for this check.
			}
			if prev, ok := allPaths[e]; ok {
				t.Errorf("path %q appears in both layer %d and %d", e, prev, i)
			}
			allPaths[e] = i
		}
	}
}
