package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfigFile(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU:           ptr(true),
			PythonVersion: ptr("3.10"),
			PythonPackages: []string{
				"tensorflow==2.12.0",
				"foo==1.0.0",
			},
			CUDA: ptr("11.8"),
		},
	}
	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
}

func TestValidateConfigFileSuccess(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU: ptr(true),
			SystemPackages: []string{
				"libgl1",
				"libglib2.0-0",
			},
			PythonVersion: ptr("3.10"),
			PythonPackages: []string{
				"torch==1.8.1",
			},
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
}

func TestValidateConfigFilePythonVersionNumerical(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU: ptr(true),
			SystemPackages: []string{
				"libgl1",
				"libglib2.0-0",
			},
			PythonVersion: ptr("3.10"),
			PythonPackages: []string{
				"torch==1.8.1",
			},
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
}

func TestValidateConfigFileNullListsAllowed(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU:            ptr(true),
			PythonVersion:  ptr("3.10"),
			SystemPackages: nil,
			PythonPackages: nil,
			Run:            nil,
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
}

func TestValidateConfigFilePredictFormat(t *testing.T) {
	// Valid predict format
	cfg := &configFile{
		Build: &buildFile{
			PythonVersion: ptr("3.10"),
		},
		Predict: ptr("predict.py:Predictor"),
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)

	// Invalid predict format
	cfg.Predict = ptr("invalid_format")
	result = ValidateConfigFile(cfg)
	require.True(t, result.HasErrors())
	require.Contains(t, result.Err().Error(), "predict.py:Predictor")
}

func TestValidateConfigFileConcurrencyType(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU:           ptr(true),
			CUDA:          ptr("11.8"),
			PythonVersion: ptr("3.11"),
			PythonPackages: []string{
				"torch==2.0.1",
			},
		},
		Predict: ptr("predict.py:Predictor"),
		Concurrency: &concurrencyFile{
			Max: ptr(5),
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
}

func TestValidateConfigFileDeprecatedPythonPackages(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			PythonVersion: ptr("3.10"),
			PythonPackages: []string{
				"torch==1.8.1",
			},
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors())
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0].Message, "requirements.txt")
}

func TestValidateConfigFileDeprecatedPreInstall(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			PythonVersion: ptr("3.10"),
			PreInstall: []string{
				"echo hello",
			},
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors())
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0].Message, "build.run")
}

func TestValidateConfigFileMissingPythonVersion(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU: ptr(true),
		},
	}

	result := ValidateConfigFile(cfg)
	require.True(t, result.HasErrors())
	require.Contains(t, result.Err().Error(), "python_version is required")
}

func TestValidateConfigFileMissingPythonVersionEmptyBuild(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{},
	}

	result := ValidateConfigFile(cfg)
	require.True(t, result.HasErrors())
	require.Contains(t, result.Err().Error(), "python_version is required")
}

func TestValidateConfigFileNilBuildSkipsPythonVersionCheck(t *testing.T) {
	cfg := &configFile{}

	result := ValidateConfigFile(cfg)
	// No build section at all should not error about python_version
	require.False(t, result.HasErrors(), "expected no errors for nil build, got: %v", result.Errors)
}

func TestValidateWeights(t *testing.T) {
	image := ptr("registry.example.com/acme/my-model")

	tests := []struct {
		name    string
		image   *string
		weights []weightFile
		wantErr string // empty means expect no error
	}{
		{
			name:  "valid with two weights",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/base"}},
				{Name: "lora", Target: "/src/lora", Source: &WeightSourceConfig{URI: "hf://acme/lora"}},
			},
		},
		{
			name:  "valid with source",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model", Exclude: []string{"*.onnx"}}},
			},
		},
		{
			name:    "missing source",
			image:   image,
			weights: []weightFile{{Name: "base", Target: "/src/weights"}},
			wantErr: "source is required",
		},
		{
			name: "weights without image",
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "image is required when weights are configured",
		},
		{
			name:  "missing name",
			image: image,
			weights: []weightFile{
				{Name: "", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "name is required",
		},
		{
			name:  "uppercase name",
			image: image,
			weights: []weightFile{
				{Name: "MyModel", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "name with spaces",
			image: image,
			weights: []weightFile{
				{Name: "my model", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "name starting with hyphen",
			image: image,
			weights: []weightFile{
				{Name: "-base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "valid name with separators",
			image: image,
			weights: []weightFile{
				{Name: "z-image.turbo_v1", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
		},
		{
			name:  "duplicate name",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
				{Name: "base", Target: "/src/other", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "duplicate weight name",
		},
		{
			name:  "missing target",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "target is required",
		},
		{
			name:  "relative target",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "target must be an absolute path",
		},
		{
			name:  "duplicate target",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
				{Name: "lora", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "duplicate weight target",
		},
		{
			name:  "overlapping targets parent then child",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
				{Name: "lora", Target: "/src/weights/lora", Source: &WeightSourceConfig{URI: "hf://acme/lora"}},
			},
			wantErr: "target overlaps with",
		},
		{
			name:  "overlapping targets child then parent",
			image: image,
			weights: []weightFile{
				{Name: "lora", Target: "/src/weights/lora", Source: &WeightSourceConfig{URI: "hf://acme/lora"}},
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model"}},
			},
			wantErr: "target overlaps with",
		},
		{
			name:  "disjoint targets no false positive",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/base"}},
				{Name: "lora", Target: "/src/weights2", Source: &WeightSourceConfig{URI: "hf://acme/lora"}},
			},
		},
		{
			name:  "valid include and exclude patterns",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{
					URI:     "hf://acme/model",
					Include: []string{"*.safetensors", "*.json"},
					Exclude: []string{"*.onnx", "*.bin"},
				}},
			},
		},
		{
			name:  "empty string in include pattern",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{
					URI:     "hf://acme/model",
					Include: []string{"*.safetensors", ""},
				}},
			},
			wantErr: "pattern must not be empty",
		},
		{
			name:  "empty string in exclude pattern",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{
					URI:     "hf://acme/model",
					Exclude: []string{""},
				}},
			},
			wantErr: "pattern must not be empty",
		},
		{
			name:  "negation pattern in include",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{
					URI:     "hf://acme/model",
					Include: []string{"!*.bin"},
				}},
			},
			wantErr: "negation patterns",
		},
		{
			name:  "negation pattern in exclude",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{
					URI:     "hf://acme/model",
					Exclude: []string{"!*.safetensors"},
				}},
			},
			wantErr: "negation patterns",
		},
		{
			name:  "whitespace-only pattern rejected after trim",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{
					URI:     "hf://acme/model",
					Include: []string{"  "},
				}},
			},
			wantErr: "pattern must not be empty",
		},
		{
			name:  "backslash in pattern rejected",
			image: image,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{
					URI:     "hf://acme/model",
					Exclude: []string{`onnx\*.bin`},
				}},
			},
			wantErr: "must use forward slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &configFile{
				Build:   &buildFile{PythonVersion: ptr("3.12")},
				Image:   tt.image,
				Weights: tt.weights,
			}
			result := ValidateConfigFile(cfg)
			if tt.wantErr == "" {
				require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
			} else {
				require.True(t, result.HasErrors())
				require.Contains(t, result.Err().Error(), tt.wantErr)
			}
		})
	}
}

// ptr returns a pointer to the given value.
func ptr[T any](v T) *T { return &v }
