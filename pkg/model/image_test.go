package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImage_IsCogModel(t *testing.T) {
	tests := []struct {
		name   string
		image  *Image
		expect bool
	}{
		{
			name:   "nil labels",
			image:  &Image{Labels: nil},
			expect: false,
		},
		{
			name:   "empty labels",
			image:  &Image{Labels: map[string]string{}},
			expect: false,
		},
		{
			name: "has cog config label",
			image: &Image{
				Labels: map[string]string{
					LabelConfig: `{"build": {}}`,
				},
			},
			expect: true,
		},
		{
			name: "has other labels but not cog config",
			image: &Image{
				Labels: map[string]string{
					"some.other.label": "value",
				},
			},
			expect: false,
		},
		{
			name: "has cog version but not config",
			image: &Image{
				Labels: map[string]string{
					LabelVersion: "0.10.0",
				},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.image.IsCogModel()
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestImage_CogVersion(t *testing.T) {
	tests := []struct {
		name   string
		image  *Image
		expect string
	}{
		{
			name:   "nil labels",
			image:  &Image{Labels: nil},
			expect: "",
		},
		{
			name:   "empty labels",
			image:  &Image{Labels: map[string]string{}},
			expect: "",
		},
		{
			name: "has version label",
			image: &Image{
				Labels: map[string]string{
					LabelVersion: "0.10.0",
				},
			},
			expect: "0.10.0",
		},
		{
			name: "has other labels but not version",
			image: &Image{
				Labels: map[string]string{
					LabelConfig: `{"build": {}}`,
				},
			},
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.image.CogVersion()
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestImage_Config(t *testing.T) {
	tests := []struct {
		name   string
		image  *Image
		expect string
	}{
		{
			name:   "nil labels",
			image:  &Image{Labels: nil},
			expect: "",
		},
		{
			name: "has config label",
			image: &Image{
				Labels: map[string]string{
					LabelConfig: `{"build": {"python_version": "3.11"}}`,
				},
			},
			expect: `{"build": {"python_version": "3.11"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.image.Config()
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestImage_OpenAPISchema(t *testing.T) {
	tests := []struct {
		name   string
		image  *Image
		expect string
	}{
		{
			name:   "nil labels",
			image:  &Image{Labels: nil},
			expect: "",
		},
		{
			name: "has openapi schema label",
			image: &Image{
				Labels: map[string]string{
					LabelOpenAPISchema: `{"openapi": "3.0.0"}`,
				},
			},
			expect: `{"openapi": "3.0.0"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.image.OpenAPISchema()
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestImageSource_Values(t *testing.T) {
	// Verify the constants have expected string values
	require.Equal(t, ImageSource("local"), ImageSourceLocal)
	require.Equal(t, ImageSource("remote"), ImageSourceRemote)
	require.Equal(t, ImageSource("build"), ImageSourceBuild)
}

func TestLabelKeys(t *testing.T) {
	// Verify label keys have expected prefixes
	require.Equal(t, "run.cog.config", LabelConfig)
	require.Equal(t, "run.cog.version", LabelVersion)
	require.Equal(t, "run.cog.openapi_schema", LabelOpenAPISchema)
	require.Equal(t, "run.cog.r8_weights_manifest", LabelWeightsManifest)
	require.Equal(t, "run.cog.r8_model_dependencies", LabelModelDependencies)
}
