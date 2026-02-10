// pkg/model/index_test.go
package model

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"
)

func TestModel_IsBundle(t *testing.T) {
	t.Run("returns false with no artifacts", func(t *testing.T) {
		m := &Model{}
		require.False(t, m.IsBundle())
	})

	t.Run("returns false with only image artifact", func(t *testing.T) {
		m := &Model{
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
			},
		}
		require.False(t, m.IsBundle())
	})

	t.Run("returns true with weight artifacts", func(t *testing.T) {
		m := &Model{
			Artifacts: []Artifact{
				&ImageArtifact{name: "model", Reference: "r8.im/user/model:latest"},
				NewWeightArtifact("w1", v1.Descriptor{}, "/tmp/w1", "/weights/w1", WeightConfig{}),
			},
		}
		require.True(t, m.IsBundle())
	})
}

func TestManifestType(t *testing.T) {
	require.Equal(t, ManifestType("image"), ManifestTypeImage)
	require.Equal(t, ManifestType("weights"), ManifestTypeWeights)
}
