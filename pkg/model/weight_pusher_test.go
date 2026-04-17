package model

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/registry"
)

// packTestLayers packs a directory containing a single file into tar
// layers and returns the layer results. Used as a fixture builder so tests
// don't each reimplement packing.
func packTestLayers(t *testing.T, filename string, content []byte) (sourceDir string, layers []LayerResult) {
	t.Helper()
	sourceDir = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, filename), content, 0o644))

	cacheDir := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))

	layers, err := Pack(context.Background(), sourceDir, &PackOptions{TempDir: cacheDir})
	require.NoError(t, err)
	require.NotEmpty(t, layers)
	return sourceDir, layers
}

// newTestWeightArtifact builds a WeightArtifact with packed layers and a
// fresh manifest descriptor, suitable for push tests. Created is pinned so
// the descriptor recorded on the artifact matches the one the pusher will
// re-compute.
func newTestWeightArtifact(t *testing.T, name, target string) *WeightArtifact {
	t.Helper()
	_, layers := packTestLayers(t, "config.json", []byte(`{"hidden_size": 768}`))

	created := time.Date(2026, 4, 16, 17, 27, 7, 0, time.UTC)
	img, err := BuildWeightManifestV1(layers, WeightManifestV1Metadata{
		Name:    name,
		Target:  target,
		Created: created,
	})
	require.NoError(t, err)

	desc, err := descriptorFromImage(img)
	require.NoError(t, err)

	wa := NewWeightArtifact(name, desc, target, layers)
	wa.Created = created
	return wa
}

func TestWeightPusher_Push_ReturnsErrorForNilArtifact(t *testing.T) {
	reg := &mockRegistry{}
	pusher := NewWeightPusher(reg)

	_, err := pusher.Push(context.Background(), "r8.im/user/model", nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "artifact is nil")
}

func TestWeightPusher_Push_ReturnsErrorForEmptyRepo(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	reg := &mockRegistry{}
	pusher := NewWeightPusher(reg)

	_, err := pusher.Push(context.Background(), "", artifact)
	require.Error(t, err)
	require.Contains(t, err.Error(), "repo is required")
}

func TestWeightPusher_Push_ReturnsErrorForEmptyLayers(t *testing.T) {
	// Empty layer set must be caught before we try to build a manifest.
	artifact := NewWeightArtifact("model-v1",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "abc"}},
		"/src/weights", nil)

	reg := &mockRegistry{}
	pusher := NewWeightPusher(reg)

	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)
	require.Error(t, err)
	require.Contains(t, err.Error(), "has no layers")
}

func TestWeightPusher_Push_PushesExpectedManifest(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")
	const refDigest = "sha256:aabbccddee112233445566778899aabb00112233445566778899aabbccddeeff"

	var pushedRef string
	var pushedImg v1.Image
	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			pushedRef = ref
			pushedImg = img
			return nil
		},
	}

	pusher := NewWeightPusher(reg)
	result, err := pusher.Push(context.Background(), "r8.im/user/model", artifact,
		WeightPushOptions{ReferenceDigest: refDigest})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Tag derives from the reference digest (12-char prefix after "sha256:").
	require.Equal(t, "r8.im/user/model:weights-model-v1-aabbccddee11", pushedRef)
	require.Equal(t, pushedRef, result.Ref)

	// Manifest shape matches spec §2.2: OCI manifest, empty config, layers
	// with standard OCI media types, artifactType on the raw manifest.
	manifest, err := pushedImg.Manifest()
	require.NoError(t, err)
	require.Equal(t, types.OCIManifestSchema1, manifest.MediaType)
	require.Equal(t, types.MediaType(MediaTypeOCIEmpty), manifest.Config.MediaType)
	require.Equal(t, emptyBlobSHA256, manifest.Config.Digest.Hex)
	require.NotEmpty(t, manifest.Layers)
	for _, layer := range manifest.Layers {
		require.Contains(t, []types.MediaType{
			types.MediaType(MediaTypeOCILayerTar),
			types.MediaType(MediaTypeOCILayerTarGzip),
		}, layer.MediaType)
	}

	// Raw manifest carries artifactType; check it.
	rawManifest, err := pushedImg.RawManifest()
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rawManifest, &raw))
	require.Equal(t, MediaTypeWeightArtifact, raw["artifactType"])

	// Manifest-level annotations per spec §2.3.
	require.Equal(t, "model-v1", manifest.Annotations[AnnotationV1WeightName])
	require.Equal(t, "/src/weights", manifest.Annotations[AnnotationV1WeightTarget])
	require.Equal(t, ReferenceTypeWeights, manifest.Annotations[AnnotationV1ReferenceType])
	require.Equal(t, refDigest, manifest.Annotations[AnnotationV1ReferenceDigest])

	// Result descriptor is populated.
	require.NotEmpty(t, result.Descriptor.Digest.Hex)
	require.Greater(t, result.Descriptor.Size, int64(0))
}

func TestWeightPusher_Push_FallsBackToManifestDigestWhenReferenceMissing(t *testing.T) {
	// Without ReferenceDigest the tag derives from the manifest digest so
	// different builds still land at distinct tags.
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	var pushedRef string
	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			pushedRef = ref
			return nil
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)
	require.NoError(t, err)

	require.Contains(t, pushedRef, "weights-model-v1-")
	require.Contains(t, pushedRef, artifact.Descriptor().Digest.Hex[:12])
}

func TestWeightPusher_Push_CustomTagOverride(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	var pushedRef string
	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			pushedRef = ref
			return nil
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact,
		WeightPushOptions{Tag: "latest"})
	require.NoError(t, err)
	require.Equal(t, "r8.im/user/model:latest", pushedRef)
}

func TestWeightPusher_Push_PropagatesPushError(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			return fmt.Errorf("unauthorized: authentication required")
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "push weight manifest")
	require.Contains(t, err.Error(), "unauthorized")
}

func TestWeightPusher_Push_PropagatesLayerError(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			return fmt.Errorf("upload failed: 503 Service Unavailable")
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "push weight layers")
	require.Contains(t, err.Error(), "503 Service Unavailable")
}

func TestWeightPusher_Push_ReportsProgressPerLayer(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	var (
		mu     sync.Mutex
		events []WeightLayerProgress
	)

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			if opts.ProgressCh != nil {
				opts.ProgressCh <- v1.Update{Complete: 500, Total: 1000}
				opts.ProgressCh <- v1.Update{Complete: 1000, Total: 1000}
			}
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact,
		WeightPushOptions{
			ProgressFn: func(p WeightLayerProgress) {
				mu.Lock()
				defer mu.Unlock()
				events = append(events, p)
			},
		})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, events)
	// Every event should carry a layer digest that matches one of the
	// artifact's layers.
	digestsSeen := map[string]bool{}
	for _, e := range events {
		digestsSeen[e.LayerDigest] = true
	}
	for _, l := range artifact.Layers {
		require.True(t, digestsSeen[l.Digest.String()],
			"expected progress for layer %s", l.Digest)
	}
}

func TestWeightPusher_Push_ForwardsRetryCallback(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	var retryEvents []WeightRetryEvent
	var mu sync.Mutex
	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			if opts.Retry != nil && opts.Retry.OnRetry != nil {
				opts.Retry.OnRetry(registry.RetryEvent{
					Attempt:     1,
					MaxAttempts: 3,
					Err:         fmt.Errorf("connection reset"),
					NextRetryIn: 2 * time.Second,
				})
			}
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact,
		WeightPushOptions{
			RetryFn: func(event WeightRetryEvent) bool {
				mu.Lock()
				defer mu.Unlock()
				retryEvents = append(retryEvents, event)
				return true
			},
		})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, retryEvents)

	ev := retryEvents[0]
	require.Contains(t, ev.Name, "model-v1")
	require.Contains(t, ev.Name, "layer sha256:")
	require.Equal(t, 1, ev.Attempt)
	require.Equal(t, 3, ev.MaxAttempts)
	require.Contains(t, ev.Err.Error(), "connection reset")
	require.Equal(t, 2*time.Second, ev.NextRetryIn)
}

func TestWeightPusher_Push_PropagatesContextCancellation(t *testing.T) {
	artifact := newTestWeightArtifact(t, "model-v1", "/src/weights")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			return ctx.Err()
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			return ctx.Err()
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(ctx, "r8.im/user/model", artifact)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWeightPusher_Push_HonoursConcurrencyLimit(t *testing.T) {
	// Pack a source with enough large files that we end up with multiple
	// layers. Since test data is small, we rely on tuning bundle_file_max
	// so every file lands in its own layer.
	sourceDir := t.TempDir()
	const n = 4
	for i := range n {
		require.NoError(t, os.WriteFile(
			filepath.Join(sourceDir, fmt.Sprintf("w-%d.safetensors", i)),
			fmt.Appendf(nil, "payload %d", i),
			0o644,
		))
	}

	cacheDir := t.TempDir()
	layers, err := Pack(context.Background(), sourceDir, &PackOptions{
		BundleFileMax: 1, // every file becomes its own layer
		TempDir:       cacheDir,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(layers), n, "expected a layer per file")

	created := time.Date(2026, 4, 16, 17, 27, 7, 0, time.UTC)
	img, err := BuildWeightManifestV1(layers, WeightManifestV1Metadata{
		Name: "model", Target: "/src/weights", Created: created,
	})
	require.NoError(t, err)
	desc, err := descriptorFromImage(img)
	require.NoError(t, err)
	artifact := NewWeightArtifact("model", desc, "/src/weights", layers)
	artifact.Created = created

	var inFlight, maxInFlight atomic.Int32
	reg := &mockRegistry{
		writeLayerFunc: func(ctx context.Context, opts registry.WriteLayerOptions) error {
			cur := inFlight.Add(1)
			for {
				old := maxInFlight.Load()
				if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			inFlight.Add(-1)
			return nil
		},
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error { return nil },
	}

	pusher := NewWeightPusher(reg)
	_, err = pusher.Push(context.Background(), "r8.im/user/model", artifact,
		WeightPushOptions{Concurrency: 2})
	require.NoError(t, err)
	require.LessOrEqual(t, int(maxInFlight.Load()), 2,
		"concurrency limit not honored")
}
