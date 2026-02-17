package registry

import (
	"context"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/registry_testhelpers"
)

func TestPushOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration tests in short mode")
	}

	// Start a test registry using testcontainers
	registry := registry_testhelpers.StartTestRegistry(t)
	registryAddr := registry.RegistryHost()

	ctx := context.Background()
	client := NewRegistryClient()

	t.Run("push image", func(t *testing.T) {
		img := empty.Image
		img, _ = mutate.Config(img, v1.Config{})

		err := client.PushImage(ctx, registryAddr+"/test/push-test:v1", img)
		require.NoError(t, err)

		// Verify it exists
		exists, err := client.Exists(ctx, registryAddr+"/test/push-test:v1")
		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("push index", func(t *testing.T) {
		img := empty.Image
		img, _ = mutate.Config(img, v1.Config{})

		// Push the child image first — PushIndex only writes the index manifest,
		// it does not recursively push child manifests/blobs.
		err := client.PushImage(ctx, registryAddr+"/test/push-idx:child", img)
		require.NoError(t, err)

		idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add:        img,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}},
		})

		err = client.PushIndex(ctx, registryAddr+"/test/push-idx:v1", idx)
		require.NoError(t, err)

		// Verify it exists
		exists, err := client.Exists(ctx, registryAddr+"/test/push-idx:v1")
		require.NoError(t, err)
		require.True(t, exists)
	})
}
