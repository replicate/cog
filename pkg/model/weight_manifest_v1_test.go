package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/registry"
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

// =============================================================================
// PushMultiLayer — happy path + error paths
// =============================================================================

func TestWeightPusher_PushMultiLayer_PushesLayersAndManifest(t *testing.T) {
	dir := t.TempDir()
	writeSrcFile(t, dir, "config.json", 128)
	writeSrcFile(t, dir, "tokenizer.json", 64)
	writeSrcFile(t, dir, "model.safetensors", 100*1024*1024)

	layers := packDir(t, dir, nil)
	require.GreaterOrEqual(t, len(layers), 2)

	var (
		mu             sync.Mutex
		writtenDigests []string
		pushedRef      string
		pushedImg      v1.Image
	)

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			d, err := opts.Layer.Digest()
			require.NoError(t, err)
			mu.Lock()
			writtenDigests = append(writtenDigests, d.String())
			mu.Unlock()
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			pushedRef = ref
			pushedImg = img
			return nil
		},
	}

	pusher := NewWeightPusher(reg)
	result, err := pusher.PushMultiLayer(context.Background(), "r8.im/user/model", defaultMeta(), layers)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Every layer should have been written exactly once.
	mu.Lock()
	gotDigests := append([]string(nil), writtenDigests...)
	mu.Unlock()
	sort.Strings(gotDigests)

	wantDigests := make([]string, 0, len(layers))
	for _, l := range layers {
		wantDigests = append(wantDigests, l.Digest.String())
	}
	sort.Strings(wantDigests)
	assert.Equal(t, wantDigests, gotDigests)

	// Manifest tag derives from the reference digest.
	assert.Equal(t, pushedRef, result.Ref)
	assert.Contains(t, pushedRef, "r8.im/user/model:weights-z-image-turbo-")
	require.NotNil(t, pushedImg)

	// Result descriptor matches the pushed image.
	d, err := pushedImg.Digest()
	require.NoError(t, err)
	assert.Equal(t, d, result.Descriptor.Digest)
	raw, err := pushedImg.RawManifest()
	require.NoError(t, err)
	assert.Equal(t, int64(len(raw)), result.Descriptor.Size)
}

func TestWeightPusher_PushMultiLayer_RejectsEmptyRepo(t *testing.T) {
	pusher := NewWeightPusher(&mockRegistry{})
	_, err := pusher.PushMultiLayer(context.Background(), "", defaultMeta(), singleSmallFileLayers(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo is required")
}

func TestWeightPusher_PushMultiLayer_RejectsEmptyLayers(t *testing.T) {
	pusher := NewWeightPusher(&mockRegistry{})
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y", defaultMeta(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one layer")
}

func TestWeightPusher_PushMultiLayer_RejectsMissingMetadata(t *testing.T) {
	pusher := NewWeightPusher(&mockRegistry{})
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y",
		WeightManifestV1Metadata{Target: "/w"}, singleSmallFileLayers(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestWeightPusher_PushMultiLayer_PropagatesLayerWriteError(t *testing.T) {
	layers := singleSmallFileLayers(t)

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			return fmt.Errorf("upload failed: 503 Service Unavailable")
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y", defaultMeta(), layers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "push weight layers")
	assert.Contains(t, err.Error(), "503 Service Unavailable")
}

func TestWeightPusher_PushMultiLayer_PropagatesManifestPushError(t *testing.T) {
	layers := singleSmallFileLayers(t)

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			return fmt.Errorf("unauthorized: authentication required")
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y", defaultMeta(), layers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "push weight manifest")
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestWeightPusher_PushMultiLayer_ReportsPerLayerProgress(t *testing.T) {
	dir := t.TempDir()
	writeSrcFile(t, dir, "a.json", 128)
	writeSrcFile(t, dir, "big.safetensors", 100*1024*1024)

	layers := packDir(t, dir, nil)
	require.Len(t, layers, 2)

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			if opts.ProgressCh != nil {
				sz, err := opts.Layer.Size()
				require.NoError(t, err)
				opts.ProgressCh <- v1.Update{Complete: sz / 2, Total: sz}
				opts.ProgressCh <- v1.Update{Complete: sz, Total: sz}
			}
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
	}

	var (
		mu       sync.Mutex
		progress []WeightMultiLayerProgress
	)
	pusher := NewWeightPusher(reg)
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y", defaultMeta(), layers,
		WeightMultiLayerPushOptions{
			ProgressFn: func(p WeightMultiLayerProgress) {
				mu.Lock()
				defer mu.Unlock()
				progress = append(progress, p)
			},
		})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	// 2 updates per layer × 2 layers = 4 updates, identified by digest.
	assert.Len(t, progress, 4)
	seen := map[string]bool{}
	for _, p := range progress {
		seen[p.LayerDigest] = true
		assert.Positive(t, p.Complete)
		assert.Positive(t, p.Total)
	}
	for _, l := range layers {
		assert.True(t, seen[l.Digest.String()], "no progress for layer %s", l.Digest)
	}
}

func TestWeightPusher_PushMultiLayer_ForwardsRetryCallback(t *testing.T) {
	layers := singleSmallFileLayers(t)

	var events []WeightRetryEvent
	var eventsMu sync.Mutex

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			if opts.Retry != nil && opts.Retry.OnRetry != nil {
				opts.Retry.OnRetry(registry.RetryEvent{
					Attempt:     2,
					MaxAttempts: 5,
					Err:         fmt.Errorf("connection reset"),
					NextRetryIn: 3 * time.Second,
				})
			}
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y", defaultMeta(), layers,
		WeightMultiLayerPushOptions{
			RetryFn: func(e WeightRetryEvent) bool {
				eventsMu.Lock()
				defer eventsMu.Unlock()
				events = append(events, e)
				return true
			},
		})
	require.NoError(t, err)

	eventsMu.Lock()
	defer eventsMu.Unlock()
	require.Len(t, events, 1)
	assert.Contains(t, events[0].Name, "z-image-turbo")
	assert.Contains(t, events[0].Name, layers[0].Digest.String())
	assert.Equal(t, 2, events[0].Attempt)
	assert.Equal(t, 5, events[0].MaxAttempts)
	assert.Contains(t, events[0].Err.Error(), "connection reset")
	assert.Equal(t, 3*time.Second, events[0].NextRetryIn)
}

func TestWeightPusher_PushMultiLayer_HonoursConcurrencyLimit(t *testing.T) {
	// Pack six small files into enough separate layers by splitting bundles at 200 B.
	dir := t.TempDir()
	for i := range 6 {
		writeSrcFile(t, dir, fmt.Sprintf("f-%d.bin", i), 150)
	}
	layers := packDir(t, dir, &PackOptions{BundleFileMax: 1024, BundleSizeMax: 200})
	require.GreaterOrEqual(t, len(layers), 3, "expected multiple bundles")

	var (
		inflight atomic.Int32
		maxSeen  atomic.Int32
	)

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			cur := inflight.Add(1)
			for {
				m := maxSeen.Load()
				if cur <= m || maxSeen.CompareAndSwap(m, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inflight.Add(-1)
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y", defaultMeta(), layers,
		WeightMultiLayerPushOptions{Concurrency: 2})
	require.NoError(t, err)

	assert.LessOrEqual(t, maxSeen.Load(), int32(2),
		"expected at most 2 concurrent uploads, saw %d", maxSeen.Load())
}

func TestWeightPusher_PushMultiLayer_CustomTag(t *testing.T) {
	layers := singleSmallFileLayers(t)

	var pushedRef string
	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error { return nil },
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			pushedRef = ref
			return nil
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.PushMultiLayer(context.Background(), "r8.im/x/y", defaultMeta(), layers,
		WeightMultiLayerPushOptions{Tag: "custom-tag"})
	require.NoError(t, err)
	assert.Equal(t, "r8.im/x/y:custom-tag", pushedRef)
}

func TestWeightPusher_PushMultiLayer_ContextCancelled(t *testing.T) {
	layers := singleSmallFileLayers(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			return ctx.Err()
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.PushMultiLayer(ctx, "r8.im/x/y", defaultMeta(), layers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}
