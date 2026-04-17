package model

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Default packing thresholds per spec §1.2.
const (
	DefaultBundleFileMax = 64 * 1024 * 1024  // 64 MB
	DefaultBundleSizeMax = 256 * 1024 * 1024 // 256 MB
)

// OCI layer media types per spec §2.1.
const (
	MediaTypeOCILayerTar     = "application/vnd.oci.image.layer.v1.tar"
	MediaTypeOCILayerTarGzip = "application/vnd.oci.image.layer.v1.tar+gzip"
)

// Annotation keys per spec §2.3 (v1 format, "run.cog.*" namespace).
const (
	AnnotationV1WeightContent    = "run.cog.weight.content"
	AnnotationV1WeightFile       = "run.cog.weight.file"
	AnnotationV1WeightSizeUncomp = "run.cog.weight.size.uncompressed"
)

// Annotation values for the content annotation.
const (
	ContentBundle = "bundle"
	ContentFile   = "file"
)

// incompressibleExts lists extensions for dense binary formats that don't
// benefit from gzip compression. Per spec §1.2.
var incompressibleExts = map[string]bool{
	".safetensors": true,
	".bin":         true,
	".gguf":        true,
	".onnx":        true,
	".parquet":     true,
	".pt":          true,
	".pth":         true,
}

// PackOptions configures the packing algorithm.
type PackOptions struct {
	// BundleFileMax is the size threshold separating small from large files.
	// Files below this are bundled; files at or above get their own layer.
	// Defaults to DefaultBundleFileMax (64 MB).
	BundleFileMax int64

	// BundleSizeMax is the maximum cumulative size of a single bundle tar.
	// Defaults to DefaultBundleSizeMax (256 MB).
	BundleSizeMax int64

	// TempDir is the directory for writing tar files. Defaults to os.TempDir().
	TempDir string
}

func (o *PackOptions) bundleFileMax() int64 {
	if o != nil && o.BundleFileMax > 0 {
		return o.BundleFileMax
	}
	return DefaultBundleFileMax
}

func (o *PackOptions) bundleSizeMax() int64 {
	if o != nil && o.BundleSizeMax > 0 {
		return o.BundleSizeMax
	}
	return DefaultBundleSizeMax
}

func (o *PackOptions) tempDir() string {
	if o != nil && o.TempDir != "" {
		return o.TempDir
	}
	return os.TempDir()
}

// LayerResult describes a packed tar layer on disk, ready for OCI manifest construction.
type LayerResult struct {
	// TarPath is the path to the tar file on disk.
	TarPath string
	// Digest is the SHA256 digest of the tar file (the OCI blob digest).
	Digest v1.Hash
	// Size is the size of the tar file on disk in bytes.
	Size int64
	// UncompressedSize is the total uncompressed size of the files in this layer.
	UncompressedSize int64
	// MediaType is the OCI media type for this layer.
	MediaType types.MediaType
	// Annotations are OCI descriptor annotations for this layer.
	Annotations map[string]string
}

// fileEntry represents a single file discovered during directory walking.
type fileEntry struct {
	// relPath is the path relative to the source directory (no leading ./ or /).
	relPath string
	// absPath is the absolute path on disk.
	absPath string
	// size is the file size in bytes.
	size int64
	// mode is the file mode (for detecting directories vs files).
	mode fs.FileMode
}

// Pack walks sourceDir and produces tar layers according to the spec §1.2 packing strategy.
// It returns a slice of LayerResult describing each tar file on disk, or an error.
//
// The caller is responsible for cleaning up the tar files in LayerResult.TarPath.
// On error, Pack removes any temp files it created before returning.
func Pack(ctx context.Context, sourceDir string, opts *PackOptions) (results []LayerResult, retErr error) {
	// On error, clean up any tar files we already wrote.
	defer func() {
		if retErr != nil {
			cleanupLayerResults(results)
		}
	}()

	// Walk the source directory to collect file entries.
	entries, err := walkSourceDir(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("walk source directory: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no files found in %s", sourceDir)
	}

	// Classify files into small (bundleable) and large (own layer).
	threshold := opts.bundleFileMax()
	var smallFiles, largeFiles []fileEntry
	for _, e := range entries {
		if e.size < threshold {
			smallFiles = append(smallFiles, e)
		} else {
			largeFiles = append(largeFiles, e)
		}
	}

	// Stable-sort small files by relative path for deterministic bundling.
	sort.SliceStable(smallFiles, func(i, j int) bool {
		return smallFiles[i].relPath < smallFiles[j].relPath
	})

	// Pack small files into bundles.
	if len(smallFiles) > 0 {
		bundleResults, err := packBundles(ctx, smallFiles, opts)
		if err != nil {
			return nil, fmt.Errorf("pack bundles: %w", err)
		}
		results = append(results, bundleResults...)
	}

	// Pack large files as individual layers.
	for _, f := range largeFiles {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := packLargeFile(f, opts)
		if err != nil {
			return nil, fmt.Errorf("pack large file %s: %w", f.relPath, err)
		}
		results = append(results, result)
	}

	return results, nil
}

// cleanupLayerResults removes tar files from completed results. Best-effort; errors are ignored.
func cleanupLayerResults(results []LayerResult) {
	for _, r := range results {
		if r.TarPath != "" {
			os.Remove(r.TarPath) //nolint:errcheck,gosec // best-effort cleanup
		}
	}
}

// walkSourceDir walks a directory tree and returns all regular file entries.
// Paths are relative to sourceDir with no leading ./ or /.
func walkSourceDir(sourceDir string) ([]fileEntry, error) {
	var entries []fileEntry

	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the .cog state directory entirely.
		if d.IsDir() && d.Name() == ".cog" {
			return filepath.SkipDir
		}

		// Only collect regular files. Symlinks are intentionally skipped — the spec
		// defines packing over concrete files only. Callers working with symlinked
		// layouts (e.g. HuggingFace cache) should resolve symlinks before packing.
		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", path, err)
		}

		// Normalize to forward slashes for tar paths.
		relPath = filepath.ToSlash(relPath)

		entries = append(entries, fileEntry{
			relPath: relPath,
			absPath: path,
			size:    info.Size(),
			mode:    info.Mode(),
		})

		return nil
	})

	return entries, err
}

// packBundles packs small files into gzip-compressed tar bundles, each up to bundleSizeMax.
func packBundles(ctx context.Context, files []fileEntry, opts *PackOptions) ([]LayerResult, error) {
	maxSize := opts.bundleSizeMax()
	tempDir := opts.tempDir()

	var results []LayerResult

	var currentFiles []fileEntry
	var currentSize int64

	flush := func() error {
		if len(currentFiles) == 0 {
			return nil
		}

		result, err := writeBundleTar(currentFiles, tempDir)
		if err != nil {
			return err
		}
		results = append(results, result)
		currentFiles = nil
		currentSize = 0
		return nil
	}

	for _, f := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// If adding this file would exceed the bundle size limit, flush the current
		// bundle first. A single file larger than maxSize still gets its own bundle
		// (currentSize == 0 skips the guard) — this is intentional since the file is
		// below BundleFileMax and must be bundled somewhere.
		if currentSize > 0 && currentSize+f.size > maxSize {
			if err := flush(); err != nil {
				return nil, err
			}
		}

		currentFiles = append(currentFiles, f)
		currentSize += f.size
	}

	// Flush remaining files.
	if err := flush(); err != nil {
		return nil, err
	}

	return results, nil
}

// writeBundleTar writes a gzip-compressed tar containing the given files.
// Returns a LayerResult with the bundle's metadata.
func writeBundleTar(files []fileEntry, tempDir string) (result LayerResult, retErr error) {
	tmpFile, err := os.CreateTemp(tempDir, "cog-bundle-*.tar.gz")
	if err != nil {
		return LayerResult{}, fmt.Errorf("create temp file: %w", err)
	}
	tarPath := tmpFile.Name()
	defer func() {
		if retErr != nil {
			tmpFile.Close() //nolint:errcheck,gosec // best-effort cleanup on error path
			os.Remove(tarPath) //nolint:errcheck,gosec // best-effort cleanup on error path
		}
	}()

	// Hash the compressed output as we write it.
	hasher := sha256.New()
	countWriter := &countingWriter{w: io.MultiWriter(tmpFile, hasher)}

	gw, err := gzip.NewWriterLevel(countWriter, gzip.BestCompression)
	if err != nil {
		return LayerResult{}, fmt.Errorf("create gzip writer: %w", err)
	}

	tw := tar.NewWriter(gw)

	var uncompressedSize int64

	// Collect all directories that need to be created.
	dirs := collectDirs(files)

	// Write directory entries first (sorted, deterministic).
	for _, dir := range dirs {
		if err := tw.WriteHeader(deterministicDirHeader(dir)); err != nil {
			return LayerResult{}, fmt.Errorf("write dir header %s: %w", dir, err)
		}
	}

	// Write file entries.
	for _, f := range files {
		if err := writeFileToTar(tw, f); err != nil {
			return LayerResult{}, fmt.Errorf("write file %s: %w", f.relPath, err)
		}
		uncompressedSize += f.size
	}

	// Close tar, gzip, and file in order.
	if err := tw.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gw.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close gzip writer: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close temp file: %w", err)
	}

	digest := v1.Hash{
		Algorithm: "sha256",
		Hex:       hex.EncodeToString(hasher.Sum(nil)),
	}

	annotations := map[string]string{
		AnnotationV1WeightContent:    ContentBundle,
		AnnotationV1WeightSizeUncomp: strconv.FormatInt(uncompressedSize, 10),
	}

	return LayerResult{
		TarPath:          tarPath,
		Digest:           digest,
		Size:             countWriter.n,
		UncompressedSize: uncompressedSize,
		MediaType:        MediaTypeOCILayerTarGzip,
		Annotations:      annotations,
	}, nil
}

// packLargeFile creates a single-entry tar layer for a large file.
// Compression is determined by the file extension.
func packLargeFile(f fileEntry, opts *PackOptions) (LayerResult, error) {
	tempDir := opts.tempDir()
	ext := strings.ToLower(filepath.Ext(f.relPath))
	compress := !incompressibleExts[ext]

	if compress {
		return writeLargeFileTarGzip(f, tempDir)
	}
	return writeLargeFileTar(f, tempDir)
}

// writeLargeFileTar writes an uncompressed single-entry tar for a large file.
func writeLargeFileTar(f fileEntry, tempDir string) (result LayerResult, retErr error) {
	tmpFile, err := os.CreateTemp(tempDir, "cog-layer-*.tar")
	if err != nil {
		return LayerResult{}, fmt.Errorf("create temp file: %w", err)
	}
	tarPath := tmpFile.Name()
	defer func() {
		if retErr != nil {
			tmpFile.Close() //nolint:errcheck,gosec // best-effort cleanup on error path
			os.Remove(tarPath) //nolint:errcheck,gosec // best-effort cleanup on error path
		}
	}()

	hasher := sha256.New()
	countWriter := &countingWriter{w: io.MultiWriter(tmpFile, hasher)}

	tw := tar.NewWriter(countWriter)

	// Write directory entries for parent dirs.
	dirs := collectDirsForPath(f.relPath)
	for _, dir := range dirs {
		if err := tw.WriteHeader(deterministicDirHeader(dir)); err != nil {
			return LayerResult{}, fmt.Errorf("write dir header %s: %w", dir, err)
		}
	}

	if err := writeFileToTar(tw, f); err != nil {
		return LayerResult{}, fmt.Errorf("write file %s: %w", f.relPath, err)
	}

	if err := tw.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close tar writer: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close temp file: %w", err)
	}

	digest := v1.Hash{
		Algorithm: "sha256",
		Hex:       hex.EncodeToString(hasher.Sum(nil)),
	}

	annotations := map[string]string{
		AnnotationV1WeightContent:    ContentFile,
		AnnotationV1WeightFile:       f.relPath,
		AnnotationV1WeightSizeUncomp: strconv.FormatInt(f.size, 10),
	}

	return LayerResult{
		TarPath:          tarPath,
		Digest:           digest,
		Size:             countWriter.n,
		UncompressedSize: f.size,
		MediaType:        MediaTypeOCILayerTar,
		Annotations:      annotations,
	}, nil
}

// writeLargeFileTarGzip writes a gzip-compressed single-entry tar for a large file.
func writeLargeFileTarGzip(f fileEntry, tempDir string) (result LayerResult, retErr error) {
	tmpFile, err := os.CreateTemp(tempDir, "cog-layer-*.tar.gz")
	if err != nil {
		return LayerResult{}, fmt.Errorf("create temp file: %w", err)
	}
	tarPath := tmpFile.Name()
	defer func() {
		if retErr != nil {
			tmpFile.Close() //nolint:errcheck,gosec // best-effort cleanup on error path
			os.Remove(tarPath) //nolint:errcheck,gosec // best-effort cleanup on error path
		}
	}()

	hasher := sha256.New()
	countWriter := &countingWriter{w: io.MultiWriter(tmpFile, hasher)}

	gw, err := gzip.NewWriterLevel(countWriter, gzip.DefaultCompression)
	if err != nil {
		return LayerResult{}, fmt.Errorf("create gzip writer: %w", err)
	}

	tw := tar.NewWriter(gw)

	// Write directory entries for parent dirs.
	dirs := collectDirsForPath(f.relPath)
	for _, dir := range dirs {
		if err := tw.WriteHeader(deterministicDirHeader(dir)); err != nil {
			return LayerResult{}, fmt.Errorf("write dir header %s: %w", dir, err)
		}
	}

	if err := writeFileToTar(tw, f); err != nil {
		return LayerResult{}, fmt.Errorf("write file %s: %w", f.relPath, err)
	}

	if err := tw.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gw.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close gzip writer: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return LayerResult{}, fmt.Errorf("close temp file: %w", err)
	}

	digest := v1.Hash{
		Algorithm: "sha256",
		Hex:       hex.EncodeToString(hasher.Sum(nil)),
	}

	annotations := map[string]string{
		AnnotationV1WeightContent:    ContentFile,
		AnnotationV1WeightFile:       f.relPath,
		AnnotationV1WeightSizeUncomp: strconv.FormatInt(f.size, 10),
	}

	return LayerResult{
		TarPath:          tarPath,
		Digest:           digest,
		Size:             countWriter.n,
		UncompressedSize: f.size,
		MediaType:        MediaTypeOCILayerTarGzip,
		Annotations:      annotations,
	}, nil
}

// unixEpoch is the Unix epoch time, used for deterministic tar headers.
var unixEpoch = time.Unix(0, 0)

// deterministicDirHeader returns a tar header for a directory with deterministic properties.
func deterministicDirHeader(name string) *tar.Header {
	return &tar.Header{
		Typeflag:   tar.TypeDir,
		Name:       name + "/",
		Mode:       0o755,
		ModTime:    unixEpoch,
		AccessTime: unixEpoch,
		ChangeTime: unixEpoch,
		Format:     tar.FormatPAX,
	}
}

// writeFileToTar writes a single file entry to a tar writer with deterministic headers.
// Per spec §1.3: PAX format, zero timestamps, uid/gid 0, 0644 perms.
func writeFileToTar(tw *tar.Writer, f fileEntry) error {
	hdr := &tar.Header{
		Typeflag:   tar.TypeReg,
		Name:       f.relPath,
		Size:       f.size,
		Mode:       0o644,
		ModTime:    unixEpoch,
		AccessTime: unixEpoch,
		ChangeTime: unixEpoch,
		Format:     tar.FormatPAX,
		// UID/GID: 0/0 — Go zero values.
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	src, err := os.Open(f.absPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer src.Close()

	if _, err := io.Copy(tw, src); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}

	return nil
}

// collectDirs returns the sorted, deduplicated set of directory paths
// needed for the given files. Each intermediate directory is included.
func collectDirs(files []fileEntry) []string {
	seen := make(map[string]bool)
	var dirs []string

	for _, f := range files {
		for _, d := range collectDirsForPath(f.relPath) {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}

	sort.Strings(dirs)
	return dirs
}

// collectDirsForPath returns all parent directory components for a relative path.
// For "a/b/c.txt" it returns ["a", "a/b"].
func collectDirsForPath(relPath string) []string {
	dir := filepath.ToSlash(filepath.Dir(relPath))
	if dir == "." || dir == "" {
		return nil
	}

	var dirs []string
	parts := strings.Split(dir, "/")
	for i := range parts {
		dirs = append(dirs, strings.Join(parts[:i+1], "/"))
	}
	return dirs
}

// countingWriter wraps a writer and counts bytes written.
type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}
