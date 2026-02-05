package model

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestImage_IsCogModel(t *testing.T) {
	tests := []struct {
		name   string
		image  *ImageArtifact
		expect bool
	}{
		{
			name:   "nil labels",
			image:  &ImageArtifact{Labels: nil},
			expect: false,
		},
		{
			name:   "empty labels",
			image:  &ImageArtifact{Labels: map[string]string{}},
			expect: false,
		},
		{
			name: "has cog config label",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelConfig: `{"build": {}}`,
				},
			},
			expect: true,
		},
		{
			name: "has other labels but not cog config",
			image: &ImageArtifact{
				Labels: map[string]string{
					"some.other.label": "value",
				},
			},
			expect: false,
		},
		{
			name: "has cog version but not config",
			image: &ImageArtifact{
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
		image  *ImageArtifact
		expect string
	}{
		{
			name:   "nil labels",
			image:  &ImageArtifact{Labels: nil},
			expect: "",
		},
		{
			name:   "empty labels",
			image:  &ImageArtifact{Labels: map[string]string{}},
			expect: "",
		},
		{
			name: "has version label",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelVersion: "0.10.0",
				},
			},
			expect: "0.10.0",
		},
		{
			name: "has other labels but not version",
			image: &ImageArtifact{
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
		image  *ImageArtifact
		expect string
	}{
		{
			name:   "nil labels",
			image:  &ImageArtifact{Labels: nil},
			expect: "",
		},
		{
			name: "has config label",
			image: &ImageArtifact{
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
		image  *ImageArtifact
		expect string
	}{
		{
			name:   "nil labels",
			image:  &ImageArtifact{Labels: nil},
			expect: "",
		},
		{
			name: "has openapi schema label",
			image: &ImageArtifact{
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
}

// =============================================================================
// Parsed accessor tests
// =============================================================================

func TestImage_ParsedConfig(t *testing.T) {
	tests := []struct {
		name        string
		image       *ImageArtifact
		expectNil   bool
		expectErr   bool
		checkConfig func(t *testing.T, cfg *config.Config)
	}{
		{
			name:      "nil labels returns nil without error",
			image:     &ImageArtifact{Labels: nil},
			expectNil: true,
			expectErr: false,
		},
		{
			name:      "empty labels returns nil without error",
			image:     &ImageArtifact{Labels: map[string]string{}},
			expectNil: true,
			expectErr: false,
		},
		{
			name: "missing config label returns nil without error",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelVersion: "0.10.0",
				},
			},
			expectNil: true,
			expectErr: false,
		},
		{
			name: "valid config JSON parses correctly",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelConfig: `{"build":{"python_version":"3.12","gpu":true},"predict":"predict.py:Predictor"}`,
				},
			},
			expectNil: false,
			expectErr: false,
			checkConfig: func(t *testing.T, cfg *config.Config) {
				require.Equal(t, "3.12", cfg.Build.PythonVersion)
				require.True(t, cfg.Build.GPU)
				require.Equal(t, "predict.py:Predictor", cfg.Predict)
			},
		},
		{
			name: "invalid JSON returns error",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelConfig: `{invalid json`,
				},
			},
			expectNil: true,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := tt.image.ParsedConfig()

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.expectNil {
				require.Nil(t, cfg)
			} else {
				require.NotNil(t, cfg)
				if tt.checkConfig != nil {
					tt.checkConfig(t, cfg)
				}
			}
		})
	}
}

func TestImage_ToModel(t *testing.T) {
	tests := []struct {
		name       string
		image      *ImageArtifact
		expectErr  error
		checkModel func(t *testing.T, m *Model)
	}{
		{
			name:      "not a cog model returns ErrNotCogModel",
			image:     &ImageArtifact{Labels: map[string]string{}},
			expectErr: ErrNotCogModel,
		},
		{
			name: "valid cog model with config and schema",
			image: &ImageArtifact{
				Reference: "my-image:latest",
				Digest:    "sha256:abc123",
				Labels: map[string]string{
					LabelConfig:        `{"build":{"python_version":"3.12"},"predict":"predict.py:Predictor"}`,
					LabelVersion:       "0.10.0",
					LabelOpenAPISchema: `{"openapi":"3.0.2","info":{"title":"Cog","version":"0.1.0"},"paths":{}}`,
				},
				Source: ImageSourceLocal,
			},
			checkModel: func(t *testing.T, m *Model) {
				require.NotNil(t, m.Image)
				require.Equal(t, "my-image:latest", m.Image.Reference)
				require.Equal(t, "0.10.0", m.CogVersion)
				require.NotNil(t, m.Config)
				require.Equal(t, "3.12", m.Config.Build.PythonVersion)
				require.NotNil(t, m.Schema)
				require.Equal(t, "Cog", m.Schema.Info.Title)
			},
		},
		{
			name: "valid cog model without schema",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelConfig:  `{"build":{}}`,
					LabelVersion: "0.10.0",
				},
			},
			checkModel: func(t *testing.T, m *Model) {
				require.NotNil(t, m.Config)
				require.Nil(t, m.Schema)
			},
		},
		{
			name: "invalid config JSON returns error",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelConfig: `{invalid`,
				},
			},
			expectErr: nil, // Will have an error, just not ErrNotCogModel
		},
		{
			name: "invalid schema JSON returns error",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelConfig:        `{"build":{}}`,
					LabelOpenAPISchema: `{invalid schema`,
				},
			},
			expectErr: nil, // Will have an error, just not ErrNotCogModel
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := tt.image.ToModel()

			if tt.expectErr != nil {
				require.ErrorIs(t, err, tt.expectErr)
				return
			}

			if tt.name == "invalid config JSON returns error" || tt.name == "invalid schema JSON returns error" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.checkModel != nil {
				tt.checkModel(t, m)
			}
		})
	}
}

func TestImage_ParsedOpenAPISchema(t *testing.T) {
	tests := []struct {
		name        string
		image       *ImageArtifact
		expectNil   bool
		expectErr   bool
		checkSchema func(t *testing.T, schema *openapi3.T)
	}{
		{
			name:      "nil labels returns nil without error",
			image:     &ImageArtifact{Labels: nil},
			expectNil: true,
			expectErr: false,
		},
		{
			name:      "empty labels returns nil without error",
			image:     &ImageArtifact{Labels: map[string]string{}},
			expectNil: true,
			expectErr: false,
		},
		{
			name: "missing schema label returns nil without error",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelConfig: `{"build":{}}`,
				},
			},
			expectNil: true,
			expectErr: false,
		},
		{
			name: "valid OpenAPI JSON parses correctly",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelOpenAPISchema: `{"openapi":"3.0.2","info":{"title":"Cog","version":"0.1.0"},"paths":{}}`,
				},
			},
			expectNil: false,
			expectErr: false,
			checkSchema: func(t *testing.T, schema *openapi3.T) {
				require.Equal(t, "3.0.2", schema.OpenAPI)
				require.Equal(t, "Cog", schema.Info.Title)
				require.Equal(t, "0.1.0", schema.Info.Version)
			},
		},
		{
			name: "invalid JSON returns error",
			image: &ImageArtifact{
				Labels: map[string]string{
					LabelOpenAPISchema: `{invalid json`,
				},
			},
			expectNil: true,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := tt.image.ParsedOpenAPISchema()

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.expectNil {
				require.Nil(t, schema)
			} else {
				require.NotNil(t, schema)
				if tt.checkSchema != nil {
					tt.checkSchema(t, schema)
				}
			}
		})
	}
}
