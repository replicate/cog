package model

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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

func TestModel_IsFast(t *testing.T) {
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
			name:   "Fast false",
			model:  &Model{Config: &config.Config{Build: &config.Build{Fast: false}}},
			expect: false,
		},
		{
			name:   "Fast true",
			model:  &Model{Config: &config.Config{Build: &config.Build{Fast: true}}},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.model.IsFast()
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

func TestRuntimeConfig(t *testing.T) {
	// Test that RuntimeConfig struct can hold expected values
	runtime := &RuntimeConfig{
		GPU:           true,
		CudaVersion:   "12.1",
		CudnnVersion:  "8.9",
		PythonVersion: "3.11",
		TorchVersion:  "2.1.0",
		Env: map[string]string{
			"CUDA_VISIBLE_DEVICES": "0",
		},
	}

	require.True(t, runtime.GPU)
	require.Equal(t, "12.1", runtime.CudaVersion)
	require.Equal(t, "8.9", runtime.CudnnVersion)
	require.Equal(t, "3.11", runtime.PythonVersion)
	require.Equal(t, "2.1.0", runtime.TorchVersion)
	require.Equal(t, "0", runtime.Env["CUDA_VISIBLE_DEVICES"])
}

func TestWeightsManifest(t *testing.T) {
	// Test that WeightsManifest struct can hold expected values
	manifest := &WeightsManifest{
		Files: []WeightFile{
			{
				Path:   "/weights/model.bin",
				Digest: "sha256:abc123",
				Size:   1024 * 1024 * 100, // 100MB
				URL:    "",
			},
			{
				Path:   "/weights/config.json",
				Digest: "sha256:def456",
				Size:   1024,
				URL:    "https://example.com/config.json",
			},
		},
	}

	require.Len(t, manifest.Files, 2)
	require.Equal(t, "/weights/model.bin", manifest.Files[0].Path)
	require.Equal(t, "sha256:abc123", manifest.Files[0].Digest)
	require.Equal(t, int64(104857600), manifest.Files[0].Size)
	require.Equal(t, "", manifest.Files[0].URL)
	require.Equal(t, "https://example.com/config.json", manifest.Files[1].URL)
}
