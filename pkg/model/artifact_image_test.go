package model

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"
)

func TestImageSpec_ImplementsArtifactSpec(t *testing.T) {
	spec := NewImageSpec("model", "r8.im/user/model:latest")

	var _ ArtifactSpec = spec // compile-time interface check

	require.Equal(t, ArtifactTypeImage, spec.Type())
	require.Equal(t, "model", spec.Name())
}

func TestImageSpec_Fields(t *testing.T) {
	spec := NewImageSpec("model", "r8.im/user/model:latest",
		WithImageSecrets([]string{"secret1", "secret2"}),
		WithImageNoCache(true),
	)

	require.Equal(t, "r8.im/user/model:latest", spec.ImageName)
	require.Equal(t, []string{"secret1", "secret2"}, spec.Secrets)
	require.True(t, spec.NoCache)
}

func TestImageSpec_DefaultFields(t *testing.T) {
	spec := NewImageSpec("model", "myimage:latest")

	require.Equal(t, "myimage:latest", spec.ImageName)
	require.Nil(t, spec.Secrets)
	require.False(t, spec.NoCache)
}

func TestImageArtifact_ImplementsArtifact(t *testing.T) {
	desc := v1.Descriptor{
		Digest: v1.Hash{Algorithm: "sha256", Hex: "abc123"},
		Size:   1024,
	}
	artifact := NewImageArtifact("model", desc, "r8.im/user/model@sha256:abc123")

	var _ Artifact = artifact // compile-time interface check

	require.Equal(t, ArtifactTypeImage, artifact.Type())
	require.Equal(t, "model", artifact.Name())
	require.Equal(t, desc, artifact.Descriptor())
}

func TestImageArtifact_Reference(t *testing.T) {
	desc := v1.Descriptor{
		Digest: v1.Hash{Algorithm: "sha256", Hex: "abc123"},
		Size:   1024,
	}
	artifact := NewImageArtifact("model", desc, "r8.im/user/model@sha256:abc123")

	require.Equal(t, "r8.im/user/model@sha256:abc123", artifact.Reference)
}
