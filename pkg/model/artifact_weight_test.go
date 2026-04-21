package model

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"
)

func TestWeightSpec_ImplementsArtifactSpec(t *testing.T) {
	spec := NewWeightSpec("my-model-weights", "/data/weights", "/src/weights")

	var _ ArtifactSpec = spec // compile-time interface check

	require.Equal(t, ArtifactTypeWeight, spec.Type())
	require.Equal(t, "my-model-weights", spec.Name())
}

func TestWeightSpec_Fields(t *testing.T) {
	spec := NewWeightSpec("llama-7b", "/data/llama-7b", "/src/weights/llama-7b")

	require.Equal(t, "/data/llama-7b", spec.Source)
	require.Equal(t, "/src/weights/llama-7b", spec.Target)
}

func TestWeightArtifact_ImplementsArtifact(t *testing.T) {
	desc := v1.Descriptor{
		Digest: v1.Hash{Algorithm: "sha256", Hex: "def456"},
		Size:   4096,
	}
	layers := []LayerResult{
		{
			TarPath:   "/tmp/layer-0.tar.gz",
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "aaa"},
			Size:      15000,
			MediaType: MediaTypeOCILayerTarGzip,
		},
	}
	artifact := NewWeightArtifact("my-weights", desc, "/src/weights", layers, "", nil)

	var _ Artifact = artifact // compile-time interface check

	require.Equal(t, ArtifactTypeWeight, artifact.Type())
	require.Equal(t, "my-weights", artifact.Name())
	require.Equal(t, desc, artifact.Descriptor())
}

func TestWeightArtifact_Fields(t *testing.T) {
	desc := v1.Descriptor{
		Digest: v1.Hash{Algorithm: "sha256", Hex: "def456"},
		Size:   4096,
	}
	layers := []LayerResult{
		{
			TarPath:   "/tmp/layer-0.tar",
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "bbb"},
			Size:      2048,
			MediaType: MediaTypeOCILayerTar,
		},
	}
	artifact := NewWeightArtifact("my-weights", desc, "/src/weights", layers, "", nil)

	require.Equal(t, "/src/weights", artifact.Target)
	require.Equal(t, layers, artifact.Layers)
	require.Empty(t, artifact.SetDigest)
	require.Nil(t, artifact.ConfigBlob)
}

func TestWeightMediaTypeConstants(t *testing.T) {
	// The artifactType on the manifest is the only v1 media type with a
	// Cog-specific name; layers use standard OCI types.
	require.Equal(t, "application/vnd.cog.weight.v1", MediaTypeWeightArtifact)
	require.Equal(t, "application/vnd.oci.image.layer.v1.tar", MediaTypeOCILayerTar)
	require.Equal(t, "application/vnd.oci.image.layer.v1.tar+gzip", MediaTypeOCILayerTarGzip)
}
