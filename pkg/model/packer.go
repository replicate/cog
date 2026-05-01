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
	"runtime"
	"sort"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/weights/store"
)

// Default packing thresholds per spec §1.2.
const (
	defaultBundleFileMax = 64 * 1024 * 1024  // 64 MB
	defaultBundleSizeMax = 256 * 1024 * 1024 // 256 MB
)

// gzip compression levels. Bundles use BestCompression (small
// text-heavy files reward extra CPU); large compressible files use
// DefaultCompression (marginal savings rarely justify the cost).
// Named constants so envelope.go references them by symbol and
// changes reach the envelope digest automatically.
const (
	gzipLevelBundle = gzip.BestCompression
	gzipLevelLarge  = gzip.DefaultCompression
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

// isGzip reports whether a layer media type is a gzip-compressed tar.
func isGzip(mt types.MediaType) bool {
	return mt == mediaTypeOCILayerTarGzip
}

// packedLayer describes one packed tar layer: enough metadata to write
// an OCI manifest descriptor, plus the layerPlan needed to re-stream
// the layer bytes on demand from a content-addressed store.
//
// There is no on-disk tar file. Layer bytes are produced by streaming
// from the store every time something asks for them — once during
// digest computation, again during push. Determinism rests on the
// store contents being immutable and the tar/compressor framing being
// byte-stable for a given (plan, store) pair.
type packedLayer struct {
	// Plan is the layerPlan that produced this layer's bytes. Held so
	// fileLayer.Compressed() can reconstruct the bytes by re-running
	// the same packing pipeline against the store.
	Plan layerPlan
	// Digest is the SHA256 digest of the tar bytes (the OCI blob digest).
	Digest v1.Hash
	// Size is the size of the tar bytes in bytes (post-compression for
	// gzip layers).
	Size int64
	// UncompressedSize is the total uncompressed size of the files in
	// this layer.
	UncompressedSize int64
	// MediaType is the OCI media type for this layer.
	MediaType types.MediaType
}

// packResult is the output of packer.execute: layer descriptors and
// per-file content digests.
type packResult struct {
	// Layers are the packed layer descriptors.
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

// packer plans and executes the build of tar layers from a weight
// source inventory.
//
// Planning (planLayers) is pure and inspectable. Execution streams
// file bytes from a content-addressed store through the tar+gzip
// pipeline to compute layer digests; no on-disk scratch is written.
// The same plan can later be replayed against the same store to
// reproduce the layer bytes for push (see fileLayer).
type packer struct {
	opts packOptions
}

// newPacker constructs a packer. A nil opts yields spec-default
// thresholds.
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

// computeLayerDigests builds each planned layer in memory by streaming
// file bytes from store through the tar+gzip pipeline into a sha256
// hasher and a byte counter. No bytes are written to disk.
//
// The store MUST already contain every file referenced by the plan;
// callers ingressFromInventory before calling this.
//
// Layers are processed concurrently (bounded by GOMAXPROCS) since each
// layer reads independent files from the store and writes to io.Discard.
//
// On success returns one packedLayer per layerPlan, with Digest, Size,
// UncompressedSize, MediaType, and the originating Plan filled in.
// The Plan field lets callers later reconstruct the layer bytes for
// push without re-walking the inventory or recomputing digests.
func (p *packer) computeLayerDigests(ctx context.Context, st store.Store, pl plan) ([]packedLayer, error) {
	if len(pl.Layers) == 0 {
		return nil, fmt.Errorf("no layers in plan")
	}

	results := make([]packedLayer, len(pl.Layers))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))

	for i, lp := range pl.Layers {
		g.Go(func() error {
			lr, err := p.streamLayer(ctx, st, lp, io.Discard)
			if err != nil {
				return err
			}
			results[i] = lr
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// streamLayer writes the tar bytes for one layer through the
// tar+(gzip?)+sha256+counter pipeline into sink. Used by both digest
// computation (sink = io.Discard) and push (sink = registry uploader,
// via fileLayer.Compressed()).
//
// The returned packedLayer carries the layer's Plan so callers that
// only need to compute the digest can later replay the same stream
// for push without holding tar bytes in memory.
func (p *packer) streamLayer(ctx context.Context, st store.Store, lp layerPlan, sink io.Writer) (packedLayer, error) {
	gzipped := isGzip(lp.MediaType)

	// Writer sandwich: tar → (compressor?) → counter → (sink + hasher).
	// counter reports on-wire (compressed) bytes; hasher feeds the
	// OCI blob digest.
	hasher := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(sink, hasher)}

	var gzw *gzip.Writer
	var tarSink io.Writer = counter
	if gzipped {
		level := gzipLevelLarge
		if len(lp.Files) > 1 {
			level = gzipLevelBundle
		}
		var err error
		gzw, err = gzip.NewWriterLevel(counter, level)
		if err != nil {
			return packedLayer{}, fmt.Errorf("create gzip writer: %w", err)
		}
		tarSink = gzw
	}

	tw := tar.NewWriter(tarSink)
	if err := writeLayer(ctx, st, tw, lp.Files); err != nil {
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

	var uncompressed int64
	for _, f := range lp.Files {
		uncompressed += f.Size
	}

	return packedLayer{
		Plan:             lp,
		Digest:           v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(hasher.Sum(nil))},
		Size:             counter.n,
		UncompressedSize: uncompressed,
		MediaType:        lp.MediaType,
	}, nil
}

// packedFilesFromPlan returns the per-file index for a fully-built set
// of layers. Each file in each layerPlan gets a packedFile pointing at
// the layer's computed digest. Output is sorted by path.
//
// Not a method on packer: this is bookkeeping over the plan + computed
// layer digests, not part of the planning or streaming logic.
func packedFilesFromPlan(layers []packedLayer) []packedFile {
	var out []packedFile
	for _, lr := range layers {
		layerDigest := lr.Digest.String()
		for _, f := range lr.Plan.Files {
			out = append(out, packedFile{
				Path:        f.Path,
				Size:        f.Size,
				Digest:      f.Digest,
				LayerDigest: layerDigest,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ingressFromInventory streams each file in inv from src into st,
// hash-verifying as bytes flow through. Files already present in the
// store are skipped — store.PutFile is idempotent and drains the
// reader to io.Discard for already-stored digests, but we don't even
// open the source for those. Open() on remote sources is expensive
// (HTTP round trip); the cheap Exists() probe avoids it.
//
// Hash mismatches surface here, loudly, instead of silently producing
// a tar whose member digest disagrees with the inventory.
func ingressFromInventory(ctx context.Context, src weightsource.Source, st store.Store, inv weightsource.Inventory) error {
	for _, f := range inv.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		ok, err := st.Exists(ctx, f.Digest)
		if err != nil {
			return fmt.Errorf("check store for %s: %w", f.Path, err)
		}
		if ok {
			continue
		}
		if err := ingressOne(ctx, src, st, f); err != nil {
			return fmt.Errorf("ingress %s: %w", f.Path, err)
		}
	}
	return nil
}

func ingressOne(ctx context.Context, src weightsource.Source, st store.Store, f weightsource.InventoryFile) error {
	rc, err := src.Open(ctx, f.Path)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer rc.Close() //nolint:errcheck // best-effort close on read path
	return st.PutFile(ctx, f.Digest, f.Size, rc)
}

// writeLayer writes the in-tar layout for a layer: deterministic
// directory entries for every parent directory referenced by any
// file, followed by the files themselves in supplied order. File
// bytes come from the content-addressed store, keyed by digest.
func writeLayer(ctx context.Context, st store.Store, tw *tar.Writer, files []weightsource.InventoryFile) error {
	for _, dir := range collectDirs(files) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tw.WriteHeader(deterministicDirHeader(dir)); err != nil {
			return fmt.Errorf("write dir header %s: %w", dir, err)
		}
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeFileToTar(ctx, st, tw, f); err != nil {
			return fmt.Errorf("write file %s: %w", f.Path, err)
		}
	}
	return nil
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
// deterministic headers (spec §1.3: PAX format, zero timestamps,
// uid/gid 0, 0644 perms). File bytes come from the store, opened by
// digest.
func writeFileToTar(ctx context.Context, st store.Store, tw *tar.Writer, f weightsource.InventoryFile) error {
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

	rc, err := openFromStore(ctx, st, f.Digest)
	if err != nil {
		return fmt.Errorf("open from store: %w", err)
	}
	defer rc.Close() //nolint:errcheck // best-effort close on read path

	if _, err := io.Copy(tw, rc); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}

	return nil
}

// openFromStore returns a ReadCloser for the file at digest, resolved
// through the store's path. Wrapping store.Path + os.Open here keeps
// the tar-write loop short and gives the store a single read-time
// hook in case backends grow more elaborate (network-attached
// containerd content store, for example).
func openFromStore(ctx context.Context, st store.Store, digest string) (io.ReadCloser, error) {
	path, err := st.Path(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("resolve store path for %s: %w", digest, err)
	}
	// gosec G304: path comes from store.Path, which validates the
	// digest and composes the path inside the store root.
	f, err := os.Open(path) //nolint:gosec // see comment above
	if err != nil {
		return nil, fmt.Errorf("open store file %s: %w", path, err)
	}
	return f, nil
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
