package model

import (
	"context"
	"fmt"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/registry"
)

// --- mock registry ---

// statusMockRegistry implements registry.Client with controllable blob existence.
type statusMockRegistry struct {
	// blobs maps "repo/digest" -> exists
	blobs map[string]bool
	// blobErr if set, BlobExists returns this error for all calls
	blobErr error
}

func newMockRegistry() *statusMockRegistry {
	return &statusMockRegistry{blobs: make(map[string]bool)}
}

func (m *statusMockRegistry) addBlob(repo, digest string) {
	m.blobs[repo+"/"+digest] = true
}

func (m *statusMockRegistry) BlobExists(_ context.Context, repo string, digest string) (bool, error) {
	if m.blobErr != nil {
		return false, m.blobErr
	}
	return m.blobs[repo+"/"+digest], nil
}

// Unused interface methods — satisfy registry.Client.
func (m *statusMockRegistry) Inspect(_ context.Context, _ string, _ *registry.Platform) (*registry.ManifestResult, error) {
	return nil, nil
}
func (m *statusMockRegistry) GetImage(_ context.Context, _ string, _ *registry.Platform) (v1.Image, error) {
	return nil, nil
}
func (m *statusMockRegistry) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *statusMockRegistry) GetDescriptor(_ context.Context, _ string) (v1.Descriptor, error) {
	return v1.Descriptor{}, nil
}
func (m *statusMockRegistry) PushImage(_ context.Context, _ string, _ v1.Image) error { return nil }
func (m *statusMockRegistry) PushIndex(_ context.Context, _ string, _ v1.ImageIndex) error {
	return nil
}
func (m *statusMockRegistry) WriteLayer(_ context.Context, _ registry.WriteLayerOptions) error {
	return nil
}

// --- helpers ---

func lockEntry(name, target, uri, digest string, layers ...WeightLockLayer) WeightLockEntry {
	return WeightLockEntry{
		Name:      name,
		Target:    target,
		Source:    WeightLockSource{URI: uri, Include: []string{}, Exclude: []string{}},
		Digest:    digest,
		SetDigest: digest,
		Layers:    layers,
	}
}

func layer(digest string, size int64) WeightLockLayer {
	return WeightLockLayer{Digest: digest, Size: size}
}

func computeStatus(t *testing.T, cfg *config.Config, lock *WeightsLock, repo string, reg registry.Client) *WeightsStatus {
	t.Helper()
	ws, err := ComputeWeightsStatus(context.Background(), cfg, lock, repo, reg)
	require.NoError(t, err)
	return ws
}

// --- config vs lockfile (no registry needed for pending/stale/orphaned) ---

func TestWeightStatuses_AllPending(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/weights/base", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
			{Name: "lora", Target: "/weights/lora"},
		},
	}

	ws := computeStatus(t, cfg, nil, "repo", newMockRegistry())
	results := ws.Results()

	require.Len(t, results, 2)
	assert.Equal(t, "base", results[0].Name)
	assert.Equal(t, "/weights/base", results[0].Target)
	assert.Equal(t, WeightStatusPending, results[0].Status)
	assert.Nil(t, results[0].LockEntry)

	assert.Equal(t, "lora", results[1].Name)
	assert.Equal(t, WeightStatusPending, results[1].Status)
}

func TestWeightStatuses_BarePathNormalization(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "parakeet", Target: "/src/weights", Source: &config.WeightSourceConfig{URI: "weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			{
				Name:   "parakeet",
				Target: "/src/weights",
				Source: WeightLockSource{URI: "file://./weights", Include: []string{}, Exclude: []string{}},
				Digest: "sha256:abc", SetDigest: "sha256:abc",
				Layers: []WeightLockLayer{layer("sha256:l1", 1024)},
			},
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	require.Len(t, ws.Results(), 1)
	assert.Equal(t, WeightStatusReady, ws.Results()[0].Status, "bare path 'weights' should match normalized 'file://./weights'")
}

func TestWeightStatuses_DotSlashPathNormalization(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "w", Target: "/w", Source: &config.WeightSourceConfig{URI: "./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("w", "/w", "file://./weights", "sha256:abc", layer("sha256:l1", 100)),
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	assert.Equal(t, WeightStatusReady, ws.Results()[0].Status)
}

func TestWeightStatuses_StaleTarget(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/weights/v2", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/weights/base", "file://./weights", "sha256:abc", layer("sha256:l1", 100)),
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusStale, ws.Results()[0].Status)
}

func TestWeightStatuses_StaleSourceURI(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./new-weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc", layer("sha256:l1", 100)),
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusStale, ws.Results()[0].Status)
}

func TestWeightStatuses_StaleIncludePatterns(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{
				URI:     "file://./weights",
				Include: []string{"*.bin"},
			}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc", layer("sha256:l1", 100)),
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusStale, ws.Results()[0].Status)
}

func TestWeightStatuses_StaleExcludePatterns(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{
				URI:     "file://./weights",
				Exclude: []string{"*.tmp"},
			}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc", layer("sha256:l1", 100)),
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusStale, ws.Results()[0].Status)
}

func TestWeightStatuses_NotStaleWithMatchingPatterns(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{
				URI:     "file://./weights",
				Include: []string{"*.bin", "*.safetensors"},
				Exclude: []string{"*.tmp"},
			}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			{
				Name:   "base",
				Target: "/w",
				Source: WeightLockSource{
					URI:     "file://./weights",
					Include: []string{"*.bin", "*.safetensors"},
					Exclude: []string{"*.tmp"},
				},
				Digest: "sha256:abc", SetDigest: "sha256:abc",
				Layers: []WeightLockLayer{layer("sha256:l1", 100)},
			},
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	assert.Equal(t, WeightStatusReady, ws.Results()[0].Status)
}

func TestWeightStatuses_CogYAMLReorderingNotStale(t *testing.T) {
	// Patterns in cog.yaml can appear in any order; what matters is the
	// set. The lockfile always stores them in canonical (sorted) form,
	// so a cog.yaml reorder against a canonical lockfile reports ready.
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{
				URI:     "file://./weights",
				Include: []string{"*.safetensors", "*.bin"},
				Exclude: []string{"*.onnx", "*.tmp"},
			}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			{
				Name:   "base",
				Target: "/w",
				Source: WeightLockSource{
					URI:     "file://./weights",
					Include: []string{"*.bin", "*.safetensors"},
					Exclude: []string{"*.onnx", "*.tmp"},
				},
				Digest: "sha256:abc", SetDigest: "sha256:abc",
				Layers: []WeightLockLayer{layer("sha256:l1", 100)},
			},
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	assert.Equal(t, WeightStatusReady, ws.Results()[0].Status)
}

func TestWeightStatuses_UnsortedLockfileIsStale(t *testing.T) {
	// A lockfile whose on-disk form does not match the canonical form
	// we would write today must report stale so the next build rewrites
	// it. Here the lockfile has unsorted include patterns — a fresh
	// build from this config would produce sorted patterns, so the
	// lockfile is out of date.
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{
				URI:     "file://./weights",
				Include: []string{"*.bin", "*.safetensors"},
			}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			{
				Name:   "base",
				Target: "/w",
				Source: WeightLockSource{
					URI:     "file://./weights",
					Include: []string{"*.safetensors", "*.bin"},
					Exclude: []string{},
				},
				Digest: "sha256:abc", SetDigest: "sha256:abc",
				Layers: []WeightLockLayer{layer("sha256:l1", 100)},
			},
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusStale, ws.Results()[0].Status)
}

func TestWeightStatuses_Orphaned(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/weights/base", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/weights/base", "file://./weights", "sha256:abc", layer("sha256:l1", 100)),
			{
				Name: "old-weight", Target: "/weights/old",
				Source: WeightLockSource{URI: "file://./old", Include: []string{}, Exclude: []string{}},
				Digest: "sha256:def", Size: 2048,
			},
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	results := ws.Results()

	require.Len(t, results, 2)
	assert.Equal(t, WeightStatusReady, results[0].Status)
	assert.Equal(t, WeightStatusOrphaned, results[1].Status)
	assert.Equal(t, int64(2048), results[1].LockEntry.Size)
}

func TestWeightStatuses_EmptyConfig(t *testing.T) {
	ws := computeStatus(t, &config.Config{}, nil, "repo", newMockRegistry())
	assert.Empty(t, ws.Results())
	assert.True(t, ws.AllReady())
}

func TestWeightStatuses_EmptyConfigWithOrphanedLockEntries(t *testing.T) {
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			{Name: "orphan", Target: "/w", Digest: "sha256:abc"},
		},
	}

	ws := computeStatus(t, &config.Config{}, lock, "repo", newMockRegistry())
	require.Len(t, ws.Results(), 1)
	assert.Equal(t, WeightStatusOrphaned, ws.Results()[0].Status)
	assert.True(t, ws.HasProblems())
}

func TestWeightStatuses_NilSourceIsStale(t *testing.T) {
	// A weight declared without a source URI is malformed and reports stale.
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w"},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			{
				Name: "base", Target: "/w",
				Source: WeightLockSource{URI: "", Include: []string{}, Exclude: []string{}},
				Digest: "sha256:abc",
			},
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusStale, ws.Results()[0].Status)
}

func TestWeightStatuses_FingerprintNotCompared(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			{
				Name: "base", Target: "/w",
				Source: WeightLockSource{
					URI: "file://./weights", Include: []string{}, Exclude: []string{},
					Fingerprint: weightsource.Fingerprint("sha256:anything"),
				},
				Digest: "sha256:abc", SetDigest: "sha256:abc",
				Layers: []WeightLockLayer{layer("sha256:l1", 100)},
			},
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	assert.Equal(t, WeightStatusReady, ws.Results()[0].Status)
}

func TestWeightStatuses_PreservesConfigOrder(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "charlie", Target: "/c"},
			{Name: "alpha", Target: "/a"},
			{Name: "bravo", Target: "/b"},
		},
	}

	ws := computeStatus(t, cfg, nil, "repo", newMockRegistry())
	results := ws.Results()

	require.Len(t, results, 3)
	assert.Equal(t, "charlie", results[0].Name)
	assert.Equal(t, "alpha", results[1].Name)
	assert.Equal(t, "bravo", results[2].Name)
}

// --- per-layer registry checks ---

func TestWeightStatuses_AllLayersPresent(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc",
				layer("sha256:l1", 1000),
				layer("sha256:l2", 2000),
				layer("sha256:l3", 3000),
			),
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")
	reg.addBlob("repo", "sha256:l2")
	reg.addBlob("repo", "sha256:l3")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	r := ws.Results()[0]

	assert.Equal(t, WeightStatusReady, r.Status)
	require.Len(t, r.Layers, 3)
	for _, l := range r.Layers {
		assert.Equal(t, LayerStatusReady, l.Status)
	}
}

func TestWeightStatuses_SomeLayersMissing(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc",
				layer("sha256:l1", 1000),
				layer("sha256:l2", 2000),
				layer("sha256:l3", 3000),
			),
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:l1")
	// l2 missing
	reg.addBlob("repo", "sha256:l3")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	r := ws.Results()[0]

	assert.Equal(t, WeightStatusIncomplete, r.Status)
	require.Len(t, r.Layers, 3)
	assert.Equal(t, LayerStatusReady, r.Layers[0].Status)
	assert.Equal(t, LayerStatusMissing, r.Layers[1].Status)
	assert.Equal(t, LayerStatusReady, r.Layers[2].Status)
}

func TestWeightStatuses_AllLayersMissing(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc",
				layer("sha256:l1", 1000),
				layer("sha256:l2", 2000),
			),
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	r := ws.Results()[0]

	assert.Equal(t, WeightStatusIncomplete, r.Status)
	for _, l := range r.Layers {
		assert.Equal(t, LayerStatusMissing, l.Status)
	}
}

func TestWeightStatuses_LayerSizesPreserved(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc",
				layer("sha256:l1", 4200000000),
				layer("sha256:l2", 800000000),
			),
		},
	}

	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	r := ws.Results()[0]

	require.Len(t, r.Layers, 2)
	assert.Equal(t, int64(4200000000), r.Layers[0].Size)
	assert.Equal(t, int64(800000000), r.Layers[1].Size)
}

func TestWeightStatuses_RegistryErrorIsIncomplete(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc",
				layer("sha256:l1", 100),
			),
		},
	}

	reg := newMockRegistry()
	reg.blobErr = fmt.Errorf("network error")

	ws := computeStatus(t, cfg, lock, "repo", reg)
	assert.Equal(t, WeightStatusIncomplete, ws.Results()[0].Status)
	assert.Equal(t, LayerStatusMissing, ws.Results()[0].Layers[0].Status)
}

func TestWeightStatuses_StaleSkipsRegistryCheck(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w/new", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w/old", "file://./weights", "sha256:abc",
				layer("sha256:l1", 100),
			),
		},
	}

	// If registry were checked, this would panic since mock has no setup.
	ws := computeStatus(t, cfg, lock, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusStale, ws.Results()[0].Status)
	assert.Nil(t, ws.Results()[0].Layers)
}

func TestWeightStatuses_PendingSkipsRegistryCheck(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "new", Target: "/w"},
		},
	}

	ws := computeStatus(t, cfg, nil, "repo", newMockRegistry())
	assert.Equal(t, WeightStatusPending, ws.Results()[0].Status)
	assert.Nil(t, ws.Results()[0].Layers)
}

func TestWeightStatuses_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "base", Target: "/w", Source: &config.WeightSourceConfig{URI: "file://./weights"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("base", "/w", "file://./weights", "sha256:abc",
				layer("sha256:l1", 100),
			),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ComputeWeightsStatus(ctx, cfg, lock, "repo", newMockRegistry())
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWeightStatuses_MixedWithAllStatuses(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "ready", Target: "/a", Source: &config.WeightSourceConfig{URI: "file://./a"}},
			{Name: "incomplete", Target: "/b", Source: &config.WeightSourceConfig{URI: "file://./b"}},
			{Name: "stale", Target: "/c/new", Source: &config.WeightSourceConfig{URI: "file://./c"}},
			{Name: "pending", Target: "/d"},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("ready", "/a", "file://./a", "sha256:aaa", layer("sha256:la", 100)),
			lockEntry("incomplete", "/b", "file://./b", "sha256:bbb", layer("sha256:lb", 100)),
			lockEntry("stale", "/c/old", "file://./c", "sha256:ccc", layer("sha256:lc", 100)),
			lockEntry("orphan", "/e", "file://./e", "sha256:eee", layer("sha256:le", 100)),
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:la") // ready has its layer
	// incomplete missing sha256:lb

	ws := computeStatus(t, cfg, lock, "repo", reg)
	results := ws.Results()

	require.Len(t, results, 5)
	assert.Equal(t, WeightStatusReady, results[0].Status)
	assert.Equal(t, WeightStatusIncomplete, results[1].Status)
	assert.Equal(t, WeightStatusStale, results[2].Status)
	assert.Equal(t, WeightStatusPending, results[3].Status)
	assert.Equal(t, WeightStatusOrphaned, results[4].Status)
}

// --- struct helpers ---

func TestWeightsStatus_ByStatus(t *testing.T) {
	cfg := &config.Config{
		Weights: []config.WeightSource{
			{Name: "a", Target: "/a", Source: &config.WeightSourceConfig{URI: "file://./a"}},
			{Name: "b", Target: "/b"},
			{Name: "c", Target: "/c", Source: &config.WeightSourceConfig{URI: "file://./c"}},
		},
	}
	lock := &WeightsLock{
		Version: 1,
		Weights: []WeightLockEntry{
			lockEntry("a", "/a", "file://./a", "sha256:aaa", layer("sha256:la", 100)),
			lockEntry("c", "/c", "file://./c", "sha256:ccc", layer("sha256:lc", 100)),
		},
	}

	reg := newMockRegistry()
	reg.addBlob("repo", "sha256:la")
	reg.addBlob("repo", "sha256:lc")

	ws := computeStatus(t, cfg, lock, "repo", reg)

	ready := ws.ByStatus(WeightStatusReady)
	assert.Len(t, ready, 2)

	pending := ws.ByStatus(WeightStatusPending)
	assert.Len(t, pending, 1)
	assert.Equal(t, "b", pending[0].Name)
}
