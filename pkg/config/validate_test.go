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
	model := ptr("registry.example.com/acme/my-model")

	tests := []struct {
		name    string
		model   *string
		weights []weightFile
		wantErr string // empty means expect no error
	}{
		{
			name:  "valid with two weights",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights"},
				{Name: "lora", Target: "/src/lora"},
			},
		},
		{
			name:  "valid with source",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: &WeightSourceConfig{URI: "hf://acme/model", Exclude: []string{"*.onnx"}}},
			},
		},
		{
			name:  "valid without source",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights"},
			},
		},
		{
			name: "missing model",
			weights: []weightFile{
				{Name: "base", Target: "/src/weights"},
			},
			wantErr: "model is required when weights are configured",
		},
		{
			name:  "missing name",
			model: model,
			weights: []weightFile{
				{Name: "", Target: "/src/weights"},
			},
			wantErr: "name is required",
		},
		{
			name:  "uppercase name",
			model: model,
			weights: []weightFile{
				{Name: "MyModel", Target: "/src/weights"},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "name with spaces",
			model: model,
			weights: []weightFile{
				{Name: "my model", Target: "/src/weights"},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "name starting with hyphen",
			model: model,
			weights: []weightFile{
				{Name: "-base", Target: "/src/weights"},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "valid name with separators",
			model: model,
			weights: []weightFile{
				{Name: "z-image.turbo_v1", Target: "/src/weights"},
			},
		},
		{
			name:  "duplicate name",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights"},
				{Name: "base", Target: "/src/other"},
			},
			wantErr: "duplicate weight name",
		},
		{
			name:  "missing target",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: ""},
			},
			wantErr: "target is required",
		},
		{
			name:  "relative target",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "src/weights"},
			},
			wantErr: "target must be an absolute path",
		},
		{
			name:  "duplicate target",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights"},
				{Name: "lora", Target: "/src/weights"},
			},
			wantErr: "duplicate weight target",
		},
		{
			name:  "overlapping targets parent then child",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights"},
				{Name: "lora", Target: "/src/weights/lora"},
			},
			wantErr: "target overlaps with",
		},
		{
			name:  "overlapping targets child then parent",
			model: model,
			weights: []weightFile{
				{Name: "lora", Target: "/src/weights/lora"},
				{Name: "base", Target: "/src/weights"},
			},
			wantErr: "target overlaps with",
		},
		{
			name:  "disjoint targets no false positive",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights"},
				{Name: "lora", Target: "/src/weights2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &configFile{
				Build:   &buildFile{PythonVersion: ptr("3.12")},
				Model:   tt.model,
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
