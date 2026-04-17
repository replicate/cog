package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightLockEntry_JSONFieldNames(t *testing.T) {
	// Lockfile entries and the on-image /.cog/weights.json have the same
	// shape per spec §3.6. Tests that want to lock that down inspect the
	// JSON directly.
	entry := WeightLockEntry{
		Name:   "z-image-turbo",
		Target: "/src/weights",
		Digest: "sha256:abc",
		Layers: []WeightLockLayer{
			{Digest: "sha256:aaa", Size: 15000000, MediaType: MediaTypeOCILayerTarGzip},
		},
	}
	require.Equal(t, "z-image-turbo", entry.Name)
	require.Equal(t, "/src/weights", entry.Target)
	require.Equal(t, "sha256:abc", entry.Digest)
	require.Len(t, entry.Layers, 1)
	require.Equal(t, MediaTypeOCILayerTarGzip, entry.Layers[0].MediaType)
}

func TestMediaTypeArtifactConstant(t *testing.T) {
	require.Equal(t, "application/vnd.cog.weight.v1", MediaTypeWeightArtifact)
}
