// pkg/model/weights_lock_test.go
package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWeightsLock(t *testing.T) {
	t.Run("parse valid lockfile", func(t *testing.T) {
		json := `{
			"version": "1",
			"created": "2026-01-30T12:00:00Z",
			"files": [
				{
					"name": "model.safetensors",
					"dest": "/cache/model.safetensors",
					"digestOriginal": "sha256:abc123",
					"digest": "sha256:def456",
					"size": 1000,
					"sizeUncompressed": 2000,
					"mediaType": "application/vnd.cog.weights.layer.v1+gzip"
				}
			]
		}`

		lock, err := ParseWeightsLock([]byte(json))
		require.NoError(t, err)
		require.Equal(t, "1", lock.Version)
		require.Len(t, lock.Files, 1)
		require.Equal(t, "model.safetensors", lock.Files[0].Name)
		require.Equal(t, "/cache/model.safetensors", lock.Files[0].Dest)
		require.Equal(t, "sha256:abc123", lock.Files[0].DigestOriginal)
		require.Equal(t, "sha256:def456", lock.Files[0].Digest)
		require.Equal(t, int64(1000), lock.Files[0].Size)
	})

	t.Run("load from file", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "weights.lock")
		content := `{"version": "1", "created": "2026-01-30T12:00:00Z", "files": []}`
		require.NoError(t, os.WriteFile(lockPath, []byte(content), 0o644))

		lock, err := LoadWeightsLock(lockPath)
		require.NoError(t, err)
		require.Equal(t, "1", lock.Version)
	})

	t.Run("save to file", func(t *testing.T) {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "weights.lock")

		lock := &WeightsLock{
			Version: "1",
			Created: time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC),
			Files: []WeightFile{
				{Name: "test.bin", Dest: "/cache/test.bin"},
			},
		}

		require.NoError(t, lock.Save(lockPath))

		loaded, err := LoadWeightsLock(lockPath)
		require.NoError(t, err)
		require.Equal(t, lock.Version, loaded.Version)
		require.Len(t, loaded.Files, 1)
	})

}
