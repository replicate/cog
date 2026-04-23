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
	DefaultBundleFileMax = 64 * 1024 * 1024  // 64 MB
	DefaultBundleSizeMax = 256 * 1024 * 1024 // 256 MB
)

// OCI layer media types per spec §2.1.
const (
	MediaTypeOCILayerTar     = "application/vnd.oci.image.layer.v1.tar"
	MediaTypeOCILayerTarGzip = "application/vnd.oci.image.layer.v1.tar+gzip"
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

func (o PackOptions) bundleFileMax() int64 {
	if o.BundleFileMax > 0 {
		return o.BundleFileMax
	}
	return DefaultBundleFileMax
}

func (o PackOptions) bundleSizeMax() int64 {
	if o.BundleSizeMax > 0 {
		return o.BundleSizeMax
	}
	return DefaultBundleSizeMax
}

func (o PackOptions) tempDir() string {
	if o.TempDir != "" {
		return o.TempDir
	}
	return os.TempDir()
}

// isGzip reports whether a layer media type is a gzip-compressed tar.
func isGzip(mt types.MediaType) bool {
	return mt == MediaTypeOCILayerTarGzip
}

// PackedLayer describes a packed tar layer on disk, ready for OCI
// manifest construction. Per spec §2.5 layer descriptors carry no
// annotations; file→layer mapping lives on PackedFile.
type PackedLayer struct {
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

// PackResult is the output of Packer.Execute: tar layers and per-file
// content digests.
type PackResult struct {
	// Layers are the packed tar layers on disk.
	Layers []PackedLayer
	// Files are per-file content digests, sorted by path.
	Files []PackedFile
}

// PackedFile records a file's path, size, content digest, and which layer it
// landed in. Used to build the config blob (§2.3) and set digest (§2.4).
type PackedFile struct {
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

// Plan describes the target layer layout for an inventory. It is a pure
// function of the inventory plus packing thresholds, so layer-assignment
// logic can be inspected and cache-probed without writing tar bytes.
type Plan struct {
	// Layers is the ordered set of layers to build. Order is
	// deterministic: bundles first (sorted small files), then large
	// files in inventory order.
	Layers []LayerPlan
}

// LayerPlan describes a single planned layer.
type LayerPlan struct {
	// Files are the inventory entries packed into this layer, in the
	// order they will appear in the tar stream. Small-file bundles
	// sort by Path; large-file layers contain a single entry.
	Files []weightsource.InventoryFile

	// MediaType is the OCI media type for the produced blob.
	MediaType types.MediaType
}

// Packer builds tar layers from a weight source inventory.
//
// Packer separates planning (pure, Plan method) from execution (I/O,
// Execute method). Callers that want the full build just call Pack;
// callers that want to inspect or cache-probe the layer layout call
// Plan first, then Execute with the same plan.
type Packer struct {
	opts PackOptions
}

// NewPacker constructs a Packer. A nil opts yields spec-default
// thresholds and a temp dir rooted at os.TempDir().
func NewPacker(opts *PackOptions) *Packer {
	var o PackOptions
	if opts != nil {
		o = *opts
	}
	return &Packer{opts: o}
}

// Plan computes the target layer layout for inv. It performs no I/O
// and does not read source bytes. The returned Plan is deterministic
// for a given (inv, opts) pair.
//
// An empty inventory yields an empty Plan. Execute rejects empty
// plans; Plan itself does not, so callers can reason about the empty
// case without invoking Execute.
func (p *Packer) Plan(inv weightsource.Inventory) Plan {
	if len(inv.Files) == 0 {
		return Plan{}
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

	var layers []LayerPlan

	// Bundle small files, flushing whenever adding the next would
	// exceed bundleMax. A lone small file larger than bundleMax still
	// gets its own bundle (guarded by currentSize > 0).
	var current []weightsource.InventoryFile
	var currentSize int64
	flush := func() {
		if len(current) == 0 {
			return
		}
		layers = append(layers, LayerPlan{
			Files:     current,
			MediaType: MediaTypeOCILayerTarGzip,
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
		mt := types.MediaType(MediaTypeOCILayerTarGzip)
		if incompressibleExts[strings.ToLower(filepath.Ext(f.Path))] {
			mt = MediaTypeOCILayerTar
		}
		layers = append(layers, LayerPlan{
			Files:     []weightsource.InventoryFile{f},
			MediaType: mt,
		})
	}

	return Plan{Layers: layers}
}

// Execute builds the tar blobs described by plan, streaming file bytes
// from src. It writes one tar file per plan layer into p.opts.TempDir
// (or os.TempDir() if unset) and returns a PackResult with per-layer and
// per-file metadata.
//
// The packer trusts the inventory digests carried on plan.Layers[i].Files;
// it does not rehash file bytes while writing. Remote sources that already
// know their digests avoid a second pass.
//
// On error Execute removes any tar files it already wrote. Successful
// layers are owned by the caller, who must delete PackedLayer.TarPath.
func (p *Packer) Execute(ctx context.Context, src weightsource.Source, plan Plan) (pr *PackResult, retErr error) {
	if len(plan.Layers) == 0 {
		return nil, fmt.Errorf("no layers in plan")
	}

	results := make([]PackedLayer, 0, len(plan.Layers))
	var packed []PackedFile

	defer func() {
		if retErr != nil {
			cleanupPackedLayers(results)
		}
	}()

	for _, lp := range plan.Layers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lr, err := p.buildLayer(ctx, src, lp)
		if err != nil {
			return nil, err
		}
		layerDigest := lr.Digest.String()
		for _, f := range lp.Files {
			packed = append(packed, PackedFile{
				Path:        f.Path,
				Size:        f.Size,
				Digest:      f.Digest,
				LayerDigest: layerDigest,
			})
		}
		results = append(results, lr)
	}

	sort.Slice(packed, func(i, j int) bool { return packed[i].Path < packed[j].Path })
	return &PackResult{Layers: results, Files: packed}, nil
}

// Pack plans and executes in one call.
func (p *Packer) Pack(ctx context.Context, src weightsource.Source, inv weightsource.Inventory) (*PackResult, error) {
	plan := p.Plan(inv)
	if len(plan.Layers) == 0 {
		// Distinguish "empty inventory" from "Execute on zero layers"
		// so the error reads naturally at this call site.
		return nil, fmt.Errorf("no files in inventory")
	}
	return p.Execute(ctx, src, plan)
}

// buildLayer writes a single planned layer to disk.
func (p *Packer) buildLayer(ctx context.Context, src weightsource.Source, lp LayerPlan) (result PackedLayer, retErr error) {
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
		return PackedLayer{}, fmt.Errorf("create temp file: %w", err)
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
			return PackedLayer{}, fmt.Errorf("create gzip writer: %w", err)
		}
		tarSink = gzw
	}

	tw := tar.NewWriter(tarSink)
	if err := writeLayer(ctx, src, tw, lp.Files); err != nil {
		return PackedLayer{}, err
	}

	if err := tw.Close(); err != nil {
		return PackedLayer{}, fmt.Errorf("close tar writer: %w", err)
	}
	if gzw != nil {
		if err := gzw.Close(); err != nil {
			return PackedLayer{}, fmt.Errorf("close gzip writer: %w", err)
		}
	}
	if err := tmpFile.Close(); err != nil {
		return PackedLayer{}, fmt.Errorf("close temp file: %w", err)
	}

	var uncompressed int64
	for _, f := range lp.Files {
		uncompressed += f.Size
	}

	return PackedLayer{
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
func cleanupPackedLayers(results []PackedLayer) {
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
