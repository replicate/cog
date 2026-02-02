package registry

import (
	"context"
	"os"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

func TestPushOperations(t *testing.T) {
	// Skip unless TEST_REGISTRY is set to a local registry address
	// e.g., TEST_REGISTRY=localhost:5000 go test ./pkg/registry/... -run TestPushOperations -v
	registryAddr := os.Getenv("TEST_REGISTRY")
	if registryAddr == "" {
		t.Skip("TEST_REGISTRY not set - run: docker run -d -p 5000:5000 registry:2")
	}

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

		idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add:        img,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}},
		})

		err := client.PushIndex(ctx, registryAddr+"/test/push-idx:v1", idx)
		require.NoError(t, err)

		// Verify it exists
		exists, err := client.Exists(ctx, registryAddr+"/test/push-idx:v1")
		require.NoError(t, err)
		require.True(t, exists)
	})
}
