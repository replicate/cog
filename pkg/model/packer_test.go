package model

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
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

	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/weights/store"
)

// packTestDir is a convenience test helper that wires a local
// directory through the new Source/Inventory + ingress +
// computeLayerDigests pipeline. It hides the boilerplate so test
// bodies can focus on packer behavior.
//
// Returns the packResult plus the store the layers reference, so
// callers can stream layer bytes back out via readLayerEntries
// (mirrors the path push uses).
func packTestDir(t *testing.T, dir string, opts *packOptions) (*packResult, *store.FileStore, error) {
	t.Helper()
	return packTestDirCtx(t, t.Context(), dir, opts)
}

// packTestDirCtx is the ctx-accepting variant of packTestDir for tests
// that need a context independent of the test lifetime (typically for
// cancellation tests).
func packTestDirCtx(t *testing.T, ctx context.Context, dir string, opts *packOptions) (*packResult, *store.FileStore, error) {
	t.Helper()
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		return nil, nil, err
	}
	src, err := weightsource.NewFileSource("file://"+dir, "")
	if err != nil {
		return nil, nil, err
	}
	inv, err := src.Inventory(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := ingressFromInventory(ctx, src, st, inv); err != nil {
		return nil, nil, err
	}
	pkr := newPacker(opts)
	pl := pkr.planLayers(inv)
	if len(pl.Layers) == 0 {
		return nil, nil, fmt.Errorf("no files in inventory")
	}
	layers, err := pkr.computeLayerDigests(ctx, st, pl)
	if err != nil {
		return nil, nil, err
	}
	return &packResult{
		Layers: layers,
		Files:  packedFilesFromPlan(layers),
	}, st, nil
}

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

// filesInLayer returns the relative paths packed into the given layer,
// derived from a packResult. The packer no longer tags layers with
// content-type annotations — the file→layer mapping lives on the
// packedFile slice instead.
func filesInLayer(pr *packResult, layerDigest string) []string {
	var out []string
	for _, f := range pr.Files {
		if f.LayerDigest == layerDigest {
			out = append(out, f.Path)
		}
	}
	sort.Strings(out)
	return out
}

// isBundleLayer reports whether a layer carries more than one file —
// i.e. it is a "bundle" rather than a single-file layer. This replaces
// the old run.cog.weight.content annotation check.
func isBundleLayer(pr *packResult, layerDigest string) bool {
	return len(filesInLayer(pr, layerDigest)) > 1
}

func TestPack_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	_, _, err := packTestDir(t, dir, nil)
	assert.ErrorContains(t, err, "no files in inventory")
}

func TestPack_SingleSmallFile(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "config.json", 100)

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTarGzip), r.MediaType)
	assert.True(t, r.Size > 0)
	assert.Equal(t, int64(100), r.UncompressedSize)
	assert.NotEmpty(t, r.Digest.Hex)
	assert.Equal(t, "sha256", r.Digest.Algorithm)

	// Single small file is a single-entry bundle layer.
	assert.Equal(t, []string{"config.json"}, filesInLayer(results, r.Digest.String()))

	// Verify tar contents.
	entries := readLayerEntries(t, r, st)
	require.Len(t, entries, 1)
	assert.Equal(t, "config.json", entries[0])
}

func TestPack_SingleLargeFile_Incompressible(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "model.safetensors", 100*1024*1024) // 100 MB

	results, _, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTar), r.MediaType)
	assert.Equal(t, []string{"model.safetensors"}, filesInLayer(results, r.Digest.String()))
	assert.Equal(t, int64(100*1024*1024), r.UncompressedSize)
}

func TestPack_SingleLargeFile_Compressible(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "model.dat", 100*1024*1024) // 100 MB, not in skip set

	results, _, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTarGzip), r.MediaType)
	assert.Equal(t, []string{"model.dat"}, filesInLayer(results, r.Digest.String()))
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

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 3) // 1 bundle + 2 large files

	// First result should be the bundle (small files come first in output).
	bundle := results.Layers[0]
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTarGzip), bundle.MediaType)
	assert.True(t, isBundleLayer(results, bundle.Digest.String()), "first layer should hold the bundled small files")

	bundleEntries := readLayerEntries(t, bundle, st)
	// Files should be sorted by path.
	assert.Equal(t, []string{"config.json", "special_tokens_map.json", "tokenizer.json"}, bundleEntries)

	// Large files should be uncompressed tars (safetensors is incompressible)
	// and carry exactly one file each.
	for _, r := range results.Layers[1:] {
		assert.Equal(t, types.MediaType(mediaTypeOCILayerTar), r.MediaType)
		assert.Len(t, filesInLayer(results, r.Digest.String()), 1, "single-file layer should contain exactly one file")
	}
}

func TestPack_NestedDirectories(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "text_encoder/config.json", 100)
	createTestFile(t, dir, "text_encoder/tokenizer.json", 200)
	createTestFile(t, dir, "vae/config.json", 150)

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1) // All small, one bundle.

	entries := readLayerEntries(t, results.Layers[0], st)
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

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	r := results.Layers[0]
	assert.Equal(t, []string{"text_encoder/model-00001.safetensors"}, filesInLayer(results, r.Digest.String()))

	entries := readLayerEntries(t, r, st)
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

	opts := &packOptions{
		BundleFileMax: 1024, // Everything is "small".
		BundleSizeMax: 20,   // Forces split: a+b in one bundle, c in another.
	}

	results, st, err := packTestDir(t, dir, opts)
	require.NoError(t, err)
	require.Len(t, results.Layers, 2)

	// Both should be gzipped bundles.
	for _, r := range results.Layers {
		assert.Equal(t, types.MediaType(mediaTypeOCILayerTarGzip), r.MediaType)
	}

	// First bundle should have a.txt and b.txt.
	entries1 := readLayerEntries(t, results.Layers[0], st)
	assert.Equal(t, []string{"a.txt", "b.txt"}, entries1)

	// Second bundle should have c.txt.
	entries2 := readLayerEntries(t, results.Layers[1], st)
	assert.Equal(t, []string{"c.txt"}, entries2)
}

func TestPack_CustomThresholds(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "small.txt", 50)
	createTestFile(t, dir, "large.bin", 200)

	opts := &packOptions{
		BundleFileMax: 100, // 50 is small, 200 is large
	}

	results, _, err := packTestDir(t, dir, opts)
	require.NoError(t, err)
	require.Len(t, results.Layers, 2)

	// Bundle for small file: single-entry bundle.
	assert.Equal(t, []string{"small.txt"}, filesInLayer(results, results.Layers[0].Digest.String()))
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTarGzip), results.Layers[0].MediaType)

	// Individual layer for large file (.bin is in incompressible set, so uncompressed).
	assert.Equal(t, []string{"large.bin"}, filesInLayer(results, results.Layers[1].Digest.String()))
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTar), results.Layers[1].MediaType)
}

func TestPack_SkipsDotCogDirectory(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "config.json", 100)
	createTestFile(t, dir, ".cog/manifest.json", 50)
	createTestFile(t, dir, ".cog/ready", 0)

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	entries := readLayerEntries(t, results.Layers[0], st)
	assert.Equal(t, []string{"config.json"}, entries)
}

func TestPack_DeterministicTarProperties(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "data.txt", 100)

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	// Stream the layer back out via fileLayer (the same path push
	// uses) and inspect tar headers.
	l := newFileLayer(t.Context(), results.Layers[0], st)
	rc, err := l.Compressed()
	require.NoError(t, err)
	defer rc.Close() //nolint:errcheck

	gr, err := gzip.NewReader(rc)
	require.NoError(t, err)
	defer gr.Close() //nolint:errcheck

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

	results1, _, err := packTestDir(t, dir, nil)
	require.NoError(t, err)

	results2, _, err := packTestDir(t, dir, nil)
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

	// Independent cancellable context: we need to cancel before the call.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := packTestDirCtx(t, ctx, dir, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPack_IncompressibleExtensions(t *testing.T) {
	tests := []struct {
		ext       string
		mediaType types.MediaType
	}{
		{".safetensors", mediaTypeOCILayerTar},
		{".bin", mediaTypeOCILayerTar},
		{".gguf", mediaTypeOCILayerTar},
		{".onnx", mediaTypeOCILayerTar},
		{".parquet", mediaTypeOCILayerTar},
		{".pt", mediaTypeOCILayerTar},
		{".pth", mediaTypeOCILayerTar},
		{".dat", mediaTypeOCILayerTarGzip},    // compressible
		{".json", mediaTypeOCILayerTarGzip},   // compressible
		{".pickle", mediaTypeOCILayerTarGzip}, // compressible
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			dir := t.TempDir()
			createTestFile(t, dir, "model"+tt.ext, 100*1024*1024)

			results, _, err := packTestDir(t, dir, nil)
			require.NoError(t, err)
			require.Len(t, results.Layers, 1)
			assert.Equal(t, tt.mediaType, results.Layers[0].MediaType)
		})
	}
}

func TestPack_FileAtExactThreshold(t *testing.T) {
	dir := t.TempDir()
	// File exactly at the threshold should be "large" (>= bundle_file_max)
	// and land in its own uncompressed-tar layer (.bin is incompressible).
	createTestFile(t, dir, "model.bin", defaultBundleFileMax)

	results, _, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTar), results.Layers[0].MediaType,
		"at-threshold large file should be a single-file uncompressed tar layer")
	assert.Equal(t, []string{"model.bin"}, filesInLayer(results, results.Layers[0].Digest.String()))
}

func TestPack_FileJustBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	// File just below the threshold should be bundled (tar+gzip).
	createTestFile(t, dir, "model.bin", defaultBundleFileMax-1)

	results, _, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTarGzip), results.Layers[0].MediaType,
		"below-threshold file should land in a bundle (tar+gzip)")
	assert.Equal(t, []string{"model.bin"}, filesInLayer(results, results.Layers[0].Digest.String()))
}

func TestPack_LayerBytesAreReproducible(t *testing.T) {
	// After cog-i12u there are no tar files on disk — layer bytes
	// are streamed on demand. Verify that streaming the same layer
	// twice produces byte-identical output (deterministic from the
	// (plan, store) pair).
	dir := t.TempDir()
	createTestFile(t, dir, "a.txt", 100)
	createTestFile(t, dir, "big.safetensors", 100*1024*1024)

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)

	for _, r := range results.Layers {
		first := readLayerTar(t, r, st)
		second := readLayerTar(t, r, st)
		assert.Equal(t, first, second,
			"streaming layer %s twice must yield identical bytes", r.Digest)

		// And the byte length matches what the packer recorded.
		assert.Equal(t, r.Size, int64(len(first)),
			"streamed layer %s size must match recorded Size", r.Digest)
	}
}

// readLayerTar streams a layer's full byte stream and returns it.
func readLayerTar(t *testing.T, lr packedLayer, st store.Store) []byte {
	t.Helper()
	l := newFileLayer(t.Context(), lr, st)
	rc, err := l.Compressed()
	require.NoError(t, err)
	defer rc.Close() //nolint:errcheck
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
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
	files := []weightsource.InventoryFile{
		{Path: "b/c/file.txt"},
		{Path: "a/file.txt"},
		{Path: "b/file.txt"},
		{Path: "root.txt"},
	}
	got := collectDirs(files)
	expected := []string{"a", "b", "b/c"}
	assert.Equal(t, expected, got, "dirs should be sorted and deduplicated")
}

// readLayerEntries streams a packedLayer's tar bytes back out via
// fileLayer (the same code path push uses) and returns the entry
// names in emission order. Handles both compressed and uncompressed
// tars based on lr.MediaType.
//
// This replaces the old readTarGzEntries/readTarEntries that took a
// path: layers no longer have on-disk paths post-cog-i12u.
func readLayerEntries(t *testing.T, lr packedLayer, st store.Store) []string {
	t.Helper()
	l := newFileLayer(t.Context(), lr, st)
	rc, err := l.Compressed()
	require.NoError(t, err)
	defer rc.Close() //nolint:errcheck // best-effort
	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	var r io.Reader = bytes.NewReader(data)
	if lr.MediaType == mediaTypeOCILayerTarGzip {
		gr, err := gzip.NewReader(r)
		require.NoError(t, err)
		defer gr.Close() //nolint:errcheck // best-effort
		r = gr
	}
	return readTarNames(t, tar.NewReader(r))
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
	files := []weightsource.InventoryFile{
		{Path: "z.txt", Size: 10},
		{Path: "a.txt", Size: 10},
		{Path: "m/b.txt", Size: 10},
		{Path: "m/a.txt", Size: 10},
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	expected := []string{"a.txt", "m/a.txt", "m/b.txt", "z.txt"}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	assert.Equal(t, expected, got)
}

func TestPack_DeepNestedDirsInLargeFile(t *testing.T) {
	dir := t.TempDir()
	createTestFile(t, dir, "a/b/c/model.safetensors", 100*1024*1024)

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)
	require.Len(t, results.Layers, 1)

	entries := readLayerEntries(t, results.Layers[0], st)
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

	results, st, err := packTestDir(t, dir, nil)
	require.NoError(t, err)

	// 1 bundle for small files + 7 individual layers for large files = 8 total.
	require.Len(t, results.Layers, 8)

	// First result is the bundle (all small files landed in one layer).
	bundle := results.Layers[0]
	assert.Equal(t, types.MediaType(mediaTypeOCILayerTarGzip), bundle.MediaType)
	assert.True(t, isBundleLayer(results, bundle.Digest.String()), "first layer should be a bundle")

	// Remaining 7 are individual files, each a standalone uncompressed
	// .safetensors layer.
	for i := 1; i <= 7; i++ {
		r := results.Layers[i]
		assert.Equal(t, types.MediaType(mediaTypeOCILayerTar), r.MediaType)
		paths := filesInLayer(results, r.Digest.String())
		require.Len(t, paths, 1, "layer %d should carry exactly one file", i)
		assert.True(t, strings.HasSuffix(paths[0], ".safetensors"),
			"layer %d file %q should be a .safetensors", i, paths[0])
	}

	// Verify no path appears in more than one layer (order-independence).
	allPaths := make(map[string]int)
	for i, r := range results.Layers {
		// readLayerEntries handles both compressed and
		// uncompressed media types.
		entries := readLayerEntries(t, r, st)
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
