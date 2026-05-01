package weights

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

func TestCheckDrift(t *testing.T) {
	t.Run("no weights in config: always passes", func(t *testing.T) {
		require.NoError(t, CheckDrift(t.TempDir(), nil))
	})

	t.Run("config and lockfile in sync: passes", func(t *testing.T) {
		dir := t.TempDir()
		lock := &lockfile.WeightsLock{
			Version: 1,
			Weights: []lockfile.WeightLockEntry{
				{
					Name:   "my-model",
					Target: "/src/weights",
					Source: lockfile.WeightLockSource{
						URI:     "file://./weights",
						Include: []string{},
						Exclude: []string{},
					},
				},
			},
		}
		require.NoError(t, lock.Save(filepath.Join(dir, lockfile.WeightsLockFilename)))

		ws := []config.WeightSource{
			{
				Name:   "my-model",
				Target: "/src/weights",
				Source: &config.WeightSourceConfig{
					URI:     "./weights",
					Include: []string{},
					Exclude: []string{},
				},
			},
		}
		require.NoError(t, CheckDrift(dir, ws))
	})

	t.Run("pending weight: errors", func(t *testing.T) {
		dir := t.TempDir()
		lock := &lockfile.WeightsLock{Version: 1, Weights: []lockfile.WeightLockEntry{}}
		require.NoError(t, lock.Save(filepath.Join(dir, lockfile.WeightsLockFilename)))

		ws := []config.WeightSource{
			{
				Name:   "new-model",
				Target: "/src/weights",
				Source: &config.WeightSourceConfig{URI: "./weights"},
			},
		}

		err := CheckDrift(dir, ws)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "weights.lock is out of sync")
		assert.Contains(t, err.Error(), "new-model")
		assert.Contains(t, err.Error(), "not imported yet")
		assert.Contains(t, err.Error(), "cog weights import")
	})

	t.Run("orphaned weight: errors", func(t *testing.T) {
		dir := t.TempDir()
		lock := &lockfile.WeightsLock{
			Version: 1,
			Weights: []lockfile.WeightLockEntry{
				{
					Name:   "kept",
					Target: "/src/kept",
					Source: lockfile.WeightLockSource{
						URI:     "file://./kept",
						Include: []string{},
						Exclude: []string{},
					},
				},
				{
					Name:   "removed",
					Target: "/src/removed",
					Source: lockfile.WeightLockSource{
						URI:     "file://./removed",
						Include: []string{},
						Exclude: []string{},
					},
				},
			},
		}
		require.NoError(t, lock.Save(filepath.Join(dir, lockfile.WeightsLockFilename)))

		ws := []config.WeightSource{
			{
				Name:   "kept",
				Target: "/src/kept",
				Source: &config.WeightSourceConfig{URI: "./kept"},
			},
		}

		err := CheckDrift(dir, ws)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "removed")
		assert.Contains(t, err.Error(), "removed from cog.yaml")
	})

	t.Run("config changed: errors", func(t *testing.T) {
		dir := t.TempDir()
		lock := &lockfile.WeightsLock{
			Version: 1,
			Weights: []lockfile.WeightLockEntry{
				{
					Name:   "my-model",
					Target: "/src/old-path",
					Source: lockfile.WeightLockSource{
						URI:     "file://./weights",
						Include: []string{},
						Exclude: []string{},
					},
				},
			},
		}
		require.NoError(t, lock.Save(filepath.Join(dir, lockfile.WeightsLockFilename)))

		ws := []config.WeightSource{
			{
				Name:   "my-model",
				Target: "/src/new-path",
				Source: &config.WeightSourceConfig{URI: "./weights"},
			},
		}

		err := CheckDrift(dir, ws)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "my-model")
		assert.Contains(t, err.Error(), "config changed")
	})

	t.Run("missing lockfile with config weights: errors", func(t *testing.T) {
		ws := []config.WeightSource{
			{
				Name:   "my-model",
				Target: "/src/weights",
				Source: &config.WeightSourceConfig{URI: "./weights"},
			},
		}

		err := CheckDrift(t.TempDir(), ws)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not imported yet")
	})

	t.Run("whitespace in patterns does not cause false drift", func(t *testing.T) {
		// Regression: the build path trims whitespace from
		// include/exclude patterns (via sortedClone), so the lockfile
		// stores trimmed values. The drift checker must also trim so
		// patterns like " *.bin " don't cause persistent false drift.
		dir := t.TempDir()
		lock := &lockfile.WeightsLock{
			Version: 1,
			Weights: []lockfile.WeightLockEntry{
				{
					Name:   "my-model",
					Target: "/src/weights",
					Source: lockfile.WeightLockSource{
						URI:     "file://./weights",
						Include: []string{"*.bin"},
						Exclude: []string{"*.tmp"},
					},
				},
			},
		}
		require.NoError(t, lock.Save(filepath.Join(dir, lockfile.WeightsLockFilename)))

		ws := []config.WeightSource{
			{
				Name:   "my-model",
				Target: "/src/weights",
				Source: &config.WeightSourceConfig{
					URI:     "./weights",
					Include: []string{"  *.bin  "},
					Exclude: []string{"  *.tmp  "},
				},
			},
		}
		require.NoError(t, CheckDrift(dir, ws),
			"whitespace-padded patterns should match trimmed lockfile values")
	})

	t.Run("corrupt lockfile: errors", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, lockfile.WeightsLockFilename),
			[]byte("not json"),
			0o644,
		))

		ws := []config.WeightSource{
			{
				Name:   "my-model",
				Target: "/src/weights",
				Source: &config.WeightSourceConfig{URI: "./weights"},
			},
		}

		err := CheckDrift(dir, ws)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load weights.lock")
	})
}
