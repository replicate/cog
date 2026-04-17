package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWeightsLock(t *testing.T) {
	t.Run("parse valid v1 lockfile", func(t *testing.T) {
		data := `{
			"version": "v1",
			"created": "2026-04-16T17:27:07Z",
			"weights": [
				{
					"name": "z-image-turbo",
					"target": "/src/weights",
					"digest": "sha256:mmm",
					"layers": [
						{
							"digest": "sha256:aaa",
							"size": 15000000,
							"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
							"annotations": {
								"run.cog.weight.content": "bundle"
							}
						},
						{
							"digest": "sha256:bbb",
							"size": 3957900840,
							"mediaType": "application/vnd.oci.image.layer.v1.tar"
						}
					]
				}
			]
		}`

		lock, err := ParseWeightsLock([]byte(data))
		require.NoError(t, err)
		require.Equal(t, WeightsLockVersion, lock.Version)
		require.Len(t, lock.Weights, 1)

		w := lock.Weights[0]
		require.Equal(t, "z-image-turbo", w.Name)
		require.Equal(t, "/src/weights", w.Target)
		require.Equal(t, "sha256:mmm", w.Digest)
		require.Len(t, w.Layers, 2)
		require.Equal(t, "sha256:aaa", w.Layers[0].Digest)
		require.Equal(t, int64(15000000), w.Layers[0].Size)
		require.Equal(t, MediaTypeOCILayerTarGzip, w.Layers[0].MediaType)
		require.Equal(t, "bundle", w.Layers[0].Annotations["run.cog.weight.content"])
		require.Equal(t, MediaTypeOCILayerTar, w.Layers[1].MediaType)
	})

	t.Run("rejects unknown version", func(t *testing.T) {
		// v0 lockfiles had version "1" (not "v1") and used a different
		// schema. Treat them as errors, not silently drop.
		data := `{"version": "1", "created": "2026-01-30T12:00:00Z", "files": []}`
		_, err := ParseWeightsLock([]byte(data))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported weights.lock version")
	})

	t.Run("load from file", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "weights.lock")
		content := `{"version": "v1", "created": "2026-04-16T17:27:07Z", "weights": []}`
		require.NoError(t, os.WriteFile(lockPath, []byte(content), 0o644))

		lock, err := LoadWeightsLock(lockPath)
		require.NoError(t, err)
		require.Equal(t, WeightsLockVersion, lock.Version)
		require.Empty(t, lock.Weights)
	})

	t.Run("save sets version if missing", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "weights.lock")

		lock := &WeightsLock{
			Created: time.Date(2026, 4, 16, 17, 27, 7, 0, time.UTC),
			Weights: []WeightLockEntry{
				{
					Name:   "z",
					Target: "/src/weights",
					Digest: "sha256:mmm",
					Layers: []WeightLockLayer{
						{Digest: "sha256:aaa", Size: 100, MediaType: MediaTypeOCILayerTar},
					},
				},
			},
		}

		require.NoError(t, lock.Save(lockPath))
		require.Equal(t, WeightsLockVersion, lock.Version, "Save fills in missing version")

		loaded, err := LoadWeightsLock(lockPath)
		require.NoError(t, err)
		require.Equal(t, lock.Version, loaded.Version)
		require.Len(t, loaded.Weights, 1)
		require.Equal(t, "z", loaded.Weights[0].Name)
	})

	t.Run("upsert replaces existing entry", func(t *testing.T) {
		lock := &WeightsLock{
			Version: WeightsLockVersion,
			Weights: []WeightLockEntry{
				{Name: "a", Target: "/a", Digest: "sha256:aaa"},
				{Name: "b", Target: "/b", Digest: "sha256:bbb"},
			},
		}

		lock.Upsert(WeightLockEntry{Name: "a", Target: "/a-new", Digest: "sha256:aaa2"})
		require.Len(t, lock.Weights, 2, "upsert replaces in place, does not append")

		got := lock.FindWeight("a")
		require.NotNil(t, got)
		require.Equal(t, "/a-new", got.Target)
		require.Equal(t, "sha256:aaa2", got.Digest)

		// b untouched
		b := lock.FindWeight("b")
		require.NotNil(t, b)
		require.Equal(t, "sha256:bbb", b.Digest)
	})

	t.Run("upsert appends new entry", func(t *testing.T) {
		lock := &WeightsLock{Version: WeightsLockVersion}
		lock.Upsert(WeightLockEntry{Name: "a", Target: "/a", Digest: "sha256:aaa"})
		lock.Upsert(WeightLockEntry{Name: "b", Target: "/b", Digest: "sha256:bbb"})

		require.Len(t, lock.Weights, 2)
		require.Equal(t, "a", lock.Weights[0].Name)
		require.Equal(t, "b", lock.Weights[1].Name)
	})

	t.Run("json round-trip preserves layer annotations", func(t *testing.T) {
		original := &WeightsLock{
			Version: WeightsLockVersion,
			Created: time.Date(2026, 4, 16, 17, 27, 7, 0, time.UTC),
			Weights: []WeightLockEntry{
				{
					Name:   "z",
					Target: "/src/weights",
					Digest: "sha256:mmm",
					Layers: []WeightLockLayer{
						{
							Digest:    "sha256:aaa",
							Size:      100,
							MediaType: MediaTypeOCILayerTarGzip,
							Annotations: map[string]string{
								"run.cog.weight.content":           "bundle",
								"run.cog.weight.size.uncompressed": "200",
							},
						},
					},
				},
			},
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded WeightsLock
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Equal(t, original.Version, decoded.Version)
		require.Equal(t, original.Weights[0].Layers[0].Annotations,
			decoded.Weights[0].Layers[0].Annotations)
	})
}
