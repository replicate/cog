package model

import (
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestModel_HasGPU(t *testing.T) {
	tests := []struct {
		name   string
		model  *Model
		expect bool
	}{
		{
			name:   "nil config",
			model:  &Model{Config: nil},
			expect: false,
		},
		{
			name:   "nil build",
			model:  &Model{Config: &config.Config{Build: nil}},
			expect: false,
		},
		{
			name:   "GPU false",
			model:  &Model{Config: &config.Config{Build: &config.Build{GPU: false}}},
			expect: false,
		},
		{
			name:   "GPU true",
			model:  &Model{Config: &config.Config{Build: &config.Build{GPU: true}}},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.model.HasGPU()
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_SchemaJSON(t *testing.T) {
	tests := []struct {
		name       string
		model      *Model
		expectNil  bool
		expectJSON string
	}{
		{
			name:      "nil schema",
			model:     &Model{Schema: nil},
			expectNil: true,
		},
		{
			name: "schema with openapi version",
			model: &Model{
				Schema: &openapi3.T{
					OpenAPI: "3.0.0",
				},
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.model.SchemaJSON()
			require.NoError(t, err)
			if tt.expectNil {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				// Verify it's valid JSON containing expected field
				require.Contains(t, string(result), `"openapi"`)
			}
		})
	}
}

func TestModel_ImageRef(t *testing.T) {
	tests := []struct {
		name   string
		model  *Model
		expect string
	}{
		{
			name:   "nil image",
			model:  &Model{Image: nil},
			expect: "",
		},
		{
			name: "with image reference",
			model: &Model{
				Image: &Image{Reference: "r8.im/user/model@sha256:abc123"},
			},
			expect: "r8.im/user/model@sha256:abc123",
		},
		{
			name: "with empty reference",
			model: &Model{
				Image: &Image{Reference: ""},
			},
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.model.ImageRef()
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestModel_GetImageArtifact(t *testing.T) {
	imgArtifact := NewImageArtifact("model",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "abc123"}, Size: 1024},
		"r8.im/user/model@sha256:abc123",
	)
	weightArtifact := NewWeightArtifact("weights",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "def456"}, Size: 4096},
		"/data/weights.bin", "/weights/model.bin",
		WeightConfig{SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "weights", Target: "/weights/model.bin", Created: time.Now()},
	)

	tests := []struct {
		name      string
		model     *Model
		expectNil bool
	}{
		{
			name:      "no artifacts",
			model:     &Model{},
			expectNil: true,
		},
		{
			name:      "nil artifacts",
			model:     &Model{Artifacts: nil},
			expectNil: true,
		},
		{
			name:      "only weight artifacts",
			model:     &Model{Artifacts: []Artifact{weightArtifact}},
			expectNil: true,
		},
		{
			name:      "has image artifact",
			model:     &Model{Artifacts: []Artifact{imgArtifact, weightArtifact}},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.model.GetImageArtifact()
			if tt.expectNil {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, ArtifactTypeImage, result.Type())
				require.Equal(t, "model", result.Name())
			}
		})
	}
}

func TestModel_WeightArtifacts(t *testing.T) {
	imgArtifact := NewImageArtifact("model",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "abc123"}, Size: 1024},
		"r8.im/user/model@sha256:abc123",
	)
	w1 := NewWeightArtifact("llama",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "w1"}, Size: 4096},
		"/data/llama.bin", "/weights/llama.bin",
		WeightConfig{SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "llama", Target: "/weights/llama.bin", Created: time.Now()},
	)
	w2 := NewWeightArtifact("embeddings",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "w2"}, Size: 2048},
		"/data/embed.bin", "/weights/embed.bin",
		WeightConfig{SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "embeddings", Target: "/weights/embed.bin", Created: time.Now()},
	)

	tests := []struct {
		name   string
		model  *Model
		expect int
	}{
		{name: "no artifacts", model: &Model{}, expect: 0},
		{name: "only image", model: &Model{Artifacts: []Artifact{imgArtifact}}, expect: 0},
		{name: "one weight", model: &Model{Artifacts: []Artifact{imgArtifact, w1}}, expect: 1},
		{name: "two weights", model: &Model{Artifacts: []Artifact{imgArtifact, w1, w2}}, expect: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.model.WeightArtifacts()
			require.Len(t, result, tt.expect)
		})
	}
}

func TestModel_ArtifactsByType(t *testing.T) {
	imgArtifact := NewImageArtifact("model",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "abc123"}, Size: 1024},
		"r8.im/user/model@sha256:abc123",
	)
	w1 := NewWeightArtifact("llama",
		v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: "w1"}, Size: 4096},
		"/data/llama.bin", "/weights/llama.bin",
		WeightConfig{SchemaVersion: "1.0", CogVersion: "0.15.0", Name: "llama", Target: "/weights/llama.bin", Created: time.Now()},
	)

	m := &Model{Artifacts: []Artifact{imgArtifact, w1}}

	images := m.ArtifactsByType(ArtifactTypeImage)
	require.Len(t, images, 1)
	require.Equal(t, "model", images[0].Name())

	weights := m.ArtifactsByType(ArtifactTypeWeight)
	require.Len(t, weights, 1)
	require.Equal(t, "llama", weights[0].Name())
}
