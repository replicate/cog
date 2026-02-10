package model

import (
	"encoding/json"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"
)

func TestWeightSpec_ImplementsArtifactSpec(t *testing.T) {
	spec := NewWeightSpec("my-model-weights", "/data/weights.bin", "/weights/model.bin")

	var _ ArtifactSpec = spec // compile-time interface check

	require.Equal(t, ArtifactTypeWeight, spec.Type())
	require.Equal(t, "my-model-weights", spec.Name())
}

func TestWeightSpec_Fields(t *testing.T) {
	spec := NewWeightSpec("llama-7b", "/data/llama-7b.safetensors", "/weights/llama-7b.safetensors")

	require.Equal(t, "/data/llama-7b.safetensors", spec.Source)
	require.Equal(t, "/weights/llama-7b.safetensors", spec.Target)
}

func TestWeightArtifact_ImplementsArtifact(t *testing.T) {
	desc := v1.Descriptor{
		Digest: v1.Hash{Algorithm: "sha256", Hex: "def456"},
		Size:   4096,
	}
	cfg := WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "my-weights",
		Target:        "/weights/model.bin",
		Created:       time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC),
	}
	artifact := NewWeightArtifact("my-weights", desc, "/data/weights.bin", "/weights/model.bin", cfg)

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
	cfg := WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "my-weights",
		Target:        "/weights/model.bin",
		Created:       time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC),
	}
	artifact := NewWeightArtifact("my-weights", desc, "/data/weights.bin", "/weights/model.bin", cfg)

	require.Equal(t, "/data/weights.bin", artifact.FilePath)
	require.Equal(t, "/weights/model.bin", artifact.Target)
	require.Equal(t, cfg, artifact.Config)
}

func TestWeightConfig_JSONRoundTrip(t *testing.T) {
	original := WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "llama-7b",
		Target:        "/weights/llama-7b",
		Created:       time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Verify JSON structure
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	require.Equal(t, "1.0", raw["schemaVersion"])
	require.Equal(t, "0.15.0", raw["cogVersion"])
	require.Equal(t, "llama-7b", raw["name"])
	require.Equal(t, "/weights/llama-7b", raw["target"])

	// Round-trip
	var decoded WeightConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	require.Equal(t, original.SchemaVersion, decoded.SchemaVersion)
	require.Equal(t, original.CogVersion, decoded.CogVersion)
	require.Equal(t, original.Name, decoded.Name)
	require.Equal(t, original.Target, decoded.Target)
	require.True(t, original.Created.Equal(decoded.Created))
}

func TestWeightMediaTypeConstants(t *testing.T) {
	// Verify media type constants have expected values
	require.Equal(t, "application/vnd.cog.weight.v1", MediaTypeWeightArtifact)
	require.Equal(t, "application/vnd.cog.weight.config.v1+json", MediaTypeWeightConfig)
	require.Equal(t, "application/vnd.cog.weight.layer.v1", MediaTypeWeightLayer)
	require.Equal(t, "application/vnd.cog.weight.layer.v1+gzip", MediaTypeWeightLayerGzip)
}
