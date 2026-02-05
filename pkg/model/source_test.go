package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestNewSourceFromConfig(t *testing.T) {
	cfg := &config.Config{
		Build: &config.Build{
			GPU:           true,
			PythonVersion: "3.11",
		},
	}
	projectDir := "/path/to/project"

	src := NewSourceFromConfig(cfg, projectDir)

	require.NotNil(t, src)
	require.Equal(t, cfg, src.Config)
	require.Equal(t, projectDir, src.ProjectDir)
}

func TestNewSourceFromConfig_NilConfig(t *testing.T) {
	src := NewSourceFromConfig(nil, "/path/to/project")

	require.NotNil(t, src)
	require.Nil(t, src.Config)
	require.Equal(t, "/path/to/project", src.ProjectDir)
}

func TestSource_ArtifactSpecs_NoWeights(t *testing.T) {
	cfg := &config.Config{
		Image: "r8.im/user/model",
		Build: &config.Build{
			GPU:           true,
			PythonVersion: "3.11",
		},
	}
	src := NewSourceFromConfig(cfg, "/path/to/project")

	specs := src.ArtifactSpecs()

	require.Len(t, specs, 1)

	// First spec should be an ImageSpec
	imgSpec, ok := specs[0].(*ImageSpec)
	require.True(t, ok, "first spec should be *ImageSpec")
	require.Equal(t, ArtifactTypeImage, imgSpec.Type())
	require.Equal(t, "model", imgSpec.Name())
	require.Equal(t, "r8.im/user/model", imgSpec.ImageName)
}

func TestSource_ArtifactSpecs_WithWeights(t *testing.T) {
	cfg := &config.Config{
		Image: "r8.im/user/model",
		Build: &config.Build{PythonVersion: "3.11"},
		Weights: []config.WeightSource{
			{Name: "llama-7b", Source: "/data/llama-7b.safetensors", Target: "/weights/llama-7b.safetensors"},
			{Name: "embeddings", Source: "/data/embeddings.bin", Target: "/weights/embeddings.bin"},
		},
	}
	src := NewSourceFromConfig(cfg, "/path/to/project")

	specs := src.ArtifactSpecs()

	require.Len(t, specs, 3) // 1 image + 2 weights

	// First is always the image
	imgSpec, ok := specs[0].(*ImageSpec)
	require.True(t, ok, "first spec should be *ImageSpec")
	require.Equal(t, ArtifactTypeImage, imgSpec.Type())

	// Remaining are weight specs in order
	w1, ok := specs[1].(*WeightSpec)
	require.True(t, ok, "second spec should be *WeightSpec")
	require.Equal(t, ArtifactTypeWeight, w1.Type())
	require.Equal(t, "llama-7b", w1.Name())
	require.Equal(t, "/data/llama-7b.safetensors", w1.Source)
	require.Equal(t, "/weights/llama-7b.safetensors", w1.Target)

	w2, ok := specs[2].(*WeightSpec)
	require.True(t, ok, "third spec should be *WeightSpec")
	require.Equal(t, "embeddings", w2.Name())
	require.Equal(t, "/data/embeddings.bin", w2.Source)
	require.Equal(t, "/weights/embeddings.bin", w2.Target)
}

func TestSource_ArtifactSpecs_EmptyImageName(t *testing.T) {
	cfg := &config.Config{
		Build: &config.Build{PythonVersion: "3.11"},
	}
	src := NewSourceFromConfig(cfg, "/path/to/project")

	specs := src.ArtifactSpecs()

	require.Len(t, specs, 1)
	imgSpec, ok := specs[0].(*ImageSpec)
	require.True(t, ok)
	require.Equal(t, "", imgSpec.ImageName) // empty is fine; BuildOptions fills it later
}

func TestSource_ArtifactSpecs_NilConfig(t *testing.T) {
	src := NewSourceFromConfig(nil, "/path/to/project")

	specs := src.ArtifactSpecs()

	require.Nil(t, specs)
}
