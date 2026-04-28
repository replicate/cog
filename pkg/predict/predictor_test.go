package predict

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/registry/registrytest"
	"github.com/replicate/cog/pkg/weights"
	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
)

// TestPredictor_Start_CleansUpMountsOnContainerStartFailure is a
// regression test: if Prepare succeeds but the docker container fails
// to start, the per-invocation mount dir under
// <projectDir>/.cog/mounts/ must be cleaned up. Callers only register
// defer Stop() on successful Start, so Start is responsible for
// cleanup on its own failure paths.
func TestPredictor_Start_CleansUpMountsOnContainerStartFailure(t *testing.T) {
	t.Parallel()

	data := []byte("x")
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	lock := &lockfile.WeightsLock{
		Version: 1,
		Weights: []lockfile.WeightLockEntry{{
			Name:   "w1",
			Target: "/src/weights/w1",
			Files:  []lockfile.WeightLockFile{{Path: "f", Size: int64(len(data)), Digest: digest, Layer: "sha256:x"}},
		}},
	}

	fs, err := store.NewFileStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, fs.PutFile(context.Background(), digest, int64(len(data)), bytes.NewReader(data)))

	projectDir := t.TempDir()
	mgr, err := weights.NewManager(weights.ManagerOptions{
		Store:      fs,
		Registry:   stubRegistry{Client: registrytest.NewMockRegistryClient()},
		Repo:       "example.com/test",
		Lock:       lock,
		ProjectDir: projectDir,
	})
	require.NoError(t, err)

	mockDocker := dockertest.NewMockCommand2(t)
	mockDocker.EXPECT().
		ContainerStart(mock.Anything, mock.Anything).
		Return("", errors.New("simulated container start failure"))

	p, err := NewPredictor(context.Background(), PredictorOptions{
		RunOptions:    command.RunOptions{Image: "fake"},
		Docker:        mockDocker,
		WeightManager: mgr,
	})
	require.NoError(t, err)

	err = p.Start(context.Background(), os.Stderr, time.Second)
	require.Error(t, err)

	// Invocation dir must be cleaned up by Start's on-error defer.
	mountRoot := filepath.Join(projectDir, ".cog", "mounts")
	entries, readErr := os.ReadDir(mountRoot)
	if readErr == nil {
		require.Empty(t, entries, "failed Start must not leave invocation dirs under %s", mountRoot)
	} else {
		require.ErrorIs(t, readErr, os.ErrNotExist)
	}
}

// stubRegistry is a minimal registry.Client the predict test can
// construct — it's never actually called because Prepare doesn't need
// the registry.
type stubRegistry struct {
	registry.Client
}
