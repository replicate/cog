package model

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/replicate/cog/pkg/model/weightsource"
)

// Default packing thresholds per spec §1.2.
const (
	defaultBundleFileMax = 64 * 1024 * 1024  // 64 MB
	defaultBundleSizeMax = 256 * 1024 * 1024 // 256 MB
)

// OCI layer media types per spec §2.1.
const (
	mediaTypeOCILayerTar     = "application/vnd.oci.image.layer.v1.tar"
	mediaTypeOCILayerTarGzip = "application/vnd.oci.image.layer.v1.tar+gzip"
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

// packOptions configures the packing algorithm.
type packOptions struct {
	// BundleFileMax is the size threshold separating small from large files.
	// Files below this are bundled; files at or above get their own layer.
	// Defaults to defaultBundleFileMax (64 MB).
	BundleFileMax int64

	// BundleSizeMax is the maximum cumulative size of a single bundle tar.
	// Defaults to defaultBundleSizeMax (256 MB).
	BundleSizeMax int64

	// TempDir is the directory for writing tar files. Defaults to os.TempDir().
	TempDir string
}

func (o packOptions) bundleFileMax() int64 {
	if o.BundleFileMax > 0 {
		return o.BundleFileMax
	}
	return defaultBundleFileMax
}

func (o packOptions) bundleSizeMax() int64 {
	if o.BundleSizeMax > 0 {
		return o.BundleSizeMax
	}
	return defaultBundleSizeMax
}

func (o packOptions) tempDir() string {
	if o.TempDir != "" {
		return o.TempDir
	}
	return os.TempDir()
}

// isGzip reports whether a layer media type is a gzip-compressed tar.
func isGzip(mt types.MediaType) bool {
	return mt == mediaTypeOCILayerTarGzip
}

// packedLayer describes a packed tar layer on disk, ready for OCI
// manifest construction. Per spec §2.5 layer descriptors carry only
// the uncompressed-size annotation; file→layer mapping lives on
// packedFile (projected into the config blob).
type packedLayer struct {
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
}

// packResult is the output of packer.execute: tar layers and per-file
// content digests.
type packResult struct {
	// Layers are the packed tar layers on disk.
	Layers []packedLayer
	// Files are per-file content digests, sorted by path.
	Files []packedFile
}

// packedFile records a file's path, size, content digest, and which layer it
// landed in. Used to build the config blob (§2.3) and set digest (§2.4).
type packedFile struct {
	// Path is the file path relative to the weight target directory.
	Path string
	// Size is the uncompressed file size in bytes.
	Size int64
	// Digest is the SHA-256 content digest of the file (hex-encoded with
	// "sha256:" prefix).
	Digest string
	// LayerDigest is the digest of the layer containing this file
	// (populated after packing).
	LayerDigest string
}

// plan describes the target layer layout for an inventory. It is a pure
// function of the inventory plus packing thresholds, so layer-assignment
// logic can be inspected and cache-probed without writing tar bytes.
type plan struct {
	// Layers is the ordered set of layers to build. Order is
	// deterministic: bundles first (sorted small files), then large
	// files in inventory order.
	Layers []layerPlan
}

// layerPlan describes a single planned layer.
type layerPlan struct {
	// Files are the inventory entries packed into this layer, in the
	// order they will appear in the tar stream. Small-file bundles
	// sort by Path; large-file layers contain a single entry.
	Files []weightsource.InventoryFile

	// MediaType is the OCI media type for the produced blob.
	MediaType types.MediaType
}

// packer builds tar layers from a weight source inventory.
//
// packer separates planning (pure, plan method) from execution (I/O,
// execute method). Callers that want the full build just call pack;
// callers that want to inspect or cache-probe the layer layout call
// plan first, then execute with the same plan.
type packer struct {
	opts packOptions
}

// newPacker constructs a packer. A nil opts yields spec-default
// thresholds and a temp dir rooted at os.TempDir().
func newPacker(opts *packOptions) *packer {
	var o packOptions
	if opts != nil {
		o = *opts
	}
	return &packer{opts: o}
}

// planLayers computes the target layer layout for inv. It performs no I/O
// and does not read source bytes. The returned plan is deterministic
// for a given (inv, opts) pair.
//
// An empty inventory yields an empty plan. execute rejects empty
// plans; planLayers itself does not, so callers can reason about the empty
// case without invoking execute.
func (p *packer) planLayers(inv weightsource.Inventory) plan {
	if len(inv.Files) == 0 {
		return plan{}
	}

	threshold := p.opts.bundleFileMax()
	bundleMax := p.opts.bundleSizeMax()

	var smallFiles, largeFiles []weightsource.InventoryFile
	for _, f := range inv.Files {
		if f.Size < threshold {
			smallFiles = append(smallFiles, f)
		} else {
			largeFiles = append(largeFiles, f)
		}
	}

	// Stable-sort small files by path for deterministic bundling.
	sort.SliceStable(smallFiles, func(i, j int) bool {
		return smallFiles[i].Path < smallFiles[j].Path
	})

	var layers []layerPlan

	// Bundle small files, flushing whenever adding the next would
	// exceed bundleMax. A lone small file larger than bundleMax still
	// gets its own bundle (guarded by currentSize > 0).
	var current []weightsource.InventoryFile
	var currentSize int64
	flush := func() {
		if len(current) == 0 {
			return
		}
		layers = append(layers, layerPlan{
			Files:     current,
			MediaType: mediaTypeOCILayerTarGzip,
		})
		current = nil
		currentSize = 0
	}
	for _, f := range smallFiles {
		if currentSize > 0 && currentSize+f.Size > bundleMax {
			flush()
		}
		current = append(current, f)
		currentSize += f.Size
	}
	flush()

	// Large files: one layer each, compressed unless the extension
	// marks the content as incompressible.
	for _, f := range largeFiles {
		mt := types.MediaType(mediaTypeOCILayerTarGzip)
		if incompressibleExts[strings.ToLower(filepath.Ext(f.Path))] {
			mt = mediaTypeOCILayerTar
		}
		layers = append(layers, layerPlan{
			Files:     []weightsource.InventoryFile{f},
			MediaType: mt,
		})
	}

	return plan{Layers: layers}
}

// execute builds the tar blobs described by plan, streaming file bytes
// from src. It writes one tar file per plan layer into p.opts.TempDir
// (or os.TempDir() if unset) and returns a packResult with per-layer and
// per-file metadata.
//
// The packer trusts the inventory digests carried on plan.Layers[i].Files;
// it does not rehash file bytes while writing. Remote sources that already
// know their digests avoid a second pass.
//
// On error execute removes any tar files it already wrote. Successful
// layers are owned by the caller, who must delete packedLayer.TarPath.
func (p *packer) execute(ctx context.Context, src weightsource.Source, pl plan) (pr *packResult, retErr error) {
	if len(pl.Layers) == 0 {
		return nil, fmt.Errorf("no layers in plan")
	}

	results := make([]packedLayer, 0, len(pl.Layers))
	var packed []packedFile

	defer func() {
		if retErr != nil {
			cleanupPackedLayers(results)
		}
	}()

	for _, lp := range pl.Layers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lr, err := p.buildLayer(ctx, src, lp)
		if err != nil {
			return nil, err
		}
		layerDigest := lr.Digest.String()
		for _, f := range lp.Files {
			packed = append(packed, packedFile{
				Path:        f.Path,
				Size:        f.Size,
				Digest:      f.Digest,
				LayerDigest: layerDigest,
			})
		}
		results = append(results, lr)
	}

	sort.Slice(packed, func(i, j int) bool { return packed[i].Path < packed[j].Path })
	return &packResult{Layers: results, Files: packed}, nil
}

// pack plans and executes in one call.
func (p *packer) pack(ctx context.Context, src weightsource.Source, inv weightsource.Inventory) (*packResult, error) {
	pl := p.planLayers(inv)
	if len(pl.Layers) == 0 {
		// Distinguish "empty inventory" from "Execute on zero layers"
		// so the error reads naturally at this call site.
		return nil, fmt.Errorf("no files in inventory")
	}
	return p.execute(ctx, src, pl)
}

// buildLayer writes a single planned layer to disk.
func (p *packer) buildLayer(ctx context.Context, src weightsource.Source, lp layerPlan) (result packedLayer, retErr error) {
	gzipped := isGzip(lp.MediaType)

	// Tmpfile prefix distinguishes bundles (many files) from single-file
	// layers when poking around tmpdirs during debugging.
	prefix := "cog-layer"
	if len(lp.Files) > 1 {
		prefix = "cog-bundle"
	}
	pattern := prefix + "-*.tar"
	if gzipped {
		pattern += ".gz"
	}

	tmpFile, err := os.CreateTemp(p.opts.tempDir(), pattern)
	if err != nil {
		return packedLayer{}, fmt.Errorf("create temp file: %w", err)
	}
	tarPath := tmpFile.Name()
	defer func() {
		if retErr != nil {
			tmpFile.Close()    //nolint:errcheck,gosec // best-effort cleanup on error path
			os.Remove(tarPath) //nolint:errcheck,gosec // best-effort cleanup on error path
		}
	}()

	// Writer sandwich: tar → (compressor?) → counter → (tmpFile + hasher).
	// The counter reports on-disk (compressed) bytes; the hasher feeds
	// the OCI blob digest.
	hasher := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(tmpFile, hasher)}

	var gzw *gzip.Writer
	var tarSink io.Writer = counter
	if gzipped {
		// BestCompression for bundles (small text-heavy files benefit
		// from tighter gzip); DefaultCompression for large compressible
		// files where the extra CPU buys little.
		level := gzip.DefaultCompression
		if len(lp.Files) > 1 {
			level = gzip.BestCompression
		}
		gzw, err = gzip.NewWriterLevel(counter, level)
		if err != nil {
			return packedLayer{}, fmt.Errorf("create gzip writer: %w", err)
		}
		tarSink = gzw
	}

	tw := tar.NewWriter(tarSink)
	if err := writeLayer(ctx, src, tw, lp.Files); err != nil {
		return packedLayer{}, err
	}

	if err := tw.Close(); err != nil {
		return packedLayer{}, fmt.Errorf("close tar writer: %w", err)
	}
	if gzw != nil {
		if err := gzw.Close(); err != nil {
			return packedLayer{}, fmt.Errorf("close gzip writer: %w", err)
		}
	}
	if err := tmpFile.Close(); err != nil {
		return packedLayer{}, fmt.Errorf("close temp file: %w", err)
	}

	var uncompressed int64
	for _, f := range lp.Files {
		uncompressed += f.Size
	}

	return packedLayer{
		TarPath:          tarPath,
		Digest:           v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(hasher.Sum(nil))},
		Size:             counter.n,
		UncompressedSize: uncompressed,
		MediaType:        lp.MediaType,
	}, nil
}

// writeLayer writes the in-tar layout for a layer: deterministic directory
// entries for every parent directory referenced by any file, followed by
// the files themselves in supplied order.
func writeLayer(ctx context.Context, src weightsource.Source, tw *tar.Writer, files []weightsource.InventoryFile) error {
	for _, dir := range collectDirs(files) {
		if err := tw.WriteHeader(deterministicDirHeader(dir)); err != nil {
			return fmt.Errorf("write dir header %s: %w", dir, err)
		}
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeFileToTar(ctx, src, tw, f); err != nil {
			return fmt.Errorf("write file %s: %w", f.Path, err)
		}
	}
	return nil
}

// cleanupPackedLayers removes tar files from completed results. Best-effort; errors are ignored.
func cleanupPackedLayers(results []packedLayer) {
	for _, r := range results {
		if r.TarPath != "" {
			os.Remove(r.TarPath) //nolint:errcheck,gosec // best-effort cleanup
		}
	}
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

// writeFileToTar writes a single file entry to a tar writer with
// deterministic headers (spec §1.3: PAX format, zero timestamps, uid/gid 0,
// 0644 perms). File bytes are pulled from src on demand.
func writeFileToTar(ctx context.Context, src weightsource.Source, tw *tar.Writer, f weightsource.InventoryFile) error {
	hdr := &tar.Header{
		Typeflag:   tar.TypeReg,
		Name:       f.Path,
		Size:       f.Size,
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

	rc, err := src.Open(ctx, f.Path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer rc.Close() //nolint:errcheck // best-effort close on read path

	if _, err := io.Copy(tw, rc); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}

	return nil
}

// collectDirs returns the sorted, deduplicated set of directory paths
// needed for the given files. Each intermediate directory is included.
func collectDirs(files []weightsource.InventoryFile) []string {
	seen := make(map[string]bool)
	var dirs []string

	for _, f := range files {
		for _, d := range collectDirsForPath(f.Path) {
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
