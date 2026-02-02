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
