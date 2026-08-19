package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfigFile(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU:           new(true),
			PythonVersion: new("3.10"),
			PythonPackages: []string{
				"tensorflow==2.12.0",
				"foo==1.0.0",
			},
			CUDA: new("11.8"),
		},
	}
	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
}

func TestValidateConfigFileIgnoresLocalArtifactPackageNames(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU:            new(true),
			PythonVersion:  new("3.10"),
			PythonPackages: []string{"tensorflow==2.15.0 source.tar.gz"},
			CUDA:           new("11.8"),
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
}

func TestValidateConfigFileSuccess(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU: new(true),
			SystemPackages: []string{
				"libgl1",
				"libglib2.0-0",
			},
			PythonVersion: new("3.10"),
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
			GPU: new(true),
			SystemPackages: []string{
				"libgl1",
				"libglib2.0-0",
			},
			PythonVersion: new("3.10"),
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
			GPU:            new(true),
			PythonVersion:  new("3.10"),
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
			PythonVersion: new("3.10"),
		},
		Predict: new("predict.py:Predictor"),
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)

	// Invalid predict format
	cfg.Predict = new("invalid_format")
	result = ValidateConfigFile(cfg)
	require.True(t, result.HasErrors())
	require.Contains(t, result.Err().Error(), "predict.py:Predictor")
}

func TestValidateConfigFileRunPredictCompatibility(t *testing.T) {
	t.Run("run is valid", func(t *testing.T) {
		cfg := &configFile{Build: &buildFile{PythonVersion: new("3.10")}, Run: new("run.py:Runner")}
		result := ValidateConfigFile(cfg)
		require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
		require.Empty(t, result.Warnings)
	})

	t.Run("run validates reference format", func(t *testing.T) {
		cfg := &configFile{Build: &buildFile{PythonVersion: new("3.10")}, Run: new("invalid_format")}
		result := ValidateConfigFile(cfg)
		require.True(t, result.HasErrors())
		require.Contains(t, result.Err().Error(), "run.py:Runner")
	})

	t.Run("predict warns", func(t *testing.T) {
		cfg := &configFile{Build: &buildFile{PythonVersion: new("3.10")}, Predict: new("predict.py:Predictor")}
		result := ValidateConfigFile(cfg)
		require.False(t, result.HasErrors())
		require.Len(t, result.Warnings, 1)
		require.Equal(t, "predict", result.Warnings[0].Field)
		require.Equal(t, "run", result.Warnings[0].Replacement)
	})

	t.Run("predict warns without build", func(t *testing.T) {
		cfg := &configFile{Predict: new("predict.py:Predictor")}
		result := ValidateConfigFile(cfg)
		require.False(t, result.HasErrors())
		require.Len(t, result.Warnings, 1)
		require.Equal(t, "predict", result.Warnings[0].Field)
		require.Equal(t, "run", result.Warnings[0].Replacement)
	})

	t.Run("both fields error", func(t *testing.T) {
		cfg := &configFile{Build: &buildFile{PythonVersion: new("3.10")}, Run: new("run.py:Runner"), Predict: new("predict.py:Predictor")}
		result := ValidateConfigFile(cfg)
		require.True(t, result.HasErrors())
		require.Contains(t, result.Err().Error(), "only one of run or predict can be set")
	})
}

func TestValidateSchemaRejectsBothRunAndPredict(t *testing.T) {
	cfg := &configFile{Build: &buildFile{PythonVersion: new("3.10")}, Run: new("run.py:Runner"), Predict: new("predict.py:Predictor")}
	err := validateSchema(cfg)
	require.Error(t, err)
}

func TestValidateConfigFileConcurrencyType(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			GPU:           new(true),
			CUDA:          new("11.8"),
			PythonVersion: new("3.11"),
			PythonPackages: []string{
				"torch==2.0.1",
			},
		},
		Predict: new("predict.py:Predictor"),
		Concurrency: &concurrencyFile{
			Max: new(5),
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors(), "expected no errors, got: %v", result.Errors)
	require.Contains(t, result.Warnings, DeprecationWarning{
		Field:       "concurrency.max",
		Replacement: "@cog.concurrent(max=...)",
		Message:     "configure prediction concurrency with @cog.concurrent on your async run() method instead",
	})
}

func TestValidateConfigFileConcurrencyDeprecationWithoutBuild(t *testing.T) {
	cfg := &configFile{
		Run: new("run.py:Runner"),
		Concurrency: &concurrencyFile{
			Max: new(2),
		},
	}

	result := ValidateConfigFile(cfg)
	require.False(t, result.HasErrors())
	require.Len(t, result.Warnings, 1)
	require.Equal(t, "concurrency.max", result.Warnings[0].Field)
}

func TestValidateObservabilityTracing(t *testing.T) {
	tests := []struct {
		name       string
		traces     *tracingFile
		wantErrors bool
	}{
		{name: "disabled", traces: &tracingFile{Enabled: new(false)}},
		{name: "default sampler", traces: &tracingFile{Enabled: new(true)}},
		{name: "ratio sampler", traces: &tracingFile{Enabled: new(true), Sampler: new("parentbased_traceidratio"), SamplerArg: new("0.25")}},
		{name: "custom header default format", traces: &tracingFile{Enabled: new(true), TraceHeader: new("x-trace")}},
		{name: "missing enabled", traces: &tracingFile{}, wantErrors: true},
		{name: "unsupported sampler", traces: &tracingFile{Enabled: new(true), Sampler: new("random")}, wantErrors: true},
		{name: "ratio on non-ratio sampler", traces: &tracingFile{Enabled: new(true), Sampler: new("always_on"), SamplerArg: new("0.5")}, wantErrors: true},
		{name: "invalid ratio", traces: &tracingFile{Enabled: new(true), Sampler: new("traceidratio"), SamplerArg: new("2")}, wantErrors: true},
		{name: "invalid header format", traces: &tracingFile{Enabled: new(true), TraceHeaderFormat: new("b3")}, wantErrors: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &configFile{Observability: &observabilityFile{Traces: test.traces}}
			result := ValidateConfigFile(cfg)
			require.Equal(t, test.wantErrors, result.HasErrors(), "errors: %v", result.Errors)
		})
	}
}

func TestValidateObservabilityConfig(t *testing.T) {
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "telemetry.py"), []byte("# telemetry"), 0o644))
	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "telemetry.py"), []byte("# telemetry"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(outsideDir, "telemetry.py"), filepath.Join(projectDir, "outside.py")))

	tests := []struct {
		name       string
		configPath string
		traces     *tracingFile
		wantError  string
	}{
		{name: "valid", configPath: "telemetry.py", traces: &tracingFile{Enabled: new(true)}},
		{name: "missing traces", configPath: "telemetry.py", wantError: "requires observability.traces.enabled"},
		{name: "disabled traces", configPath: "telemetry.py", traces: &tracingFile{Enabled: new(false)}, wantError: "requires observability.traces.enabled"},
		{name: "absolute", configPath: filepath.Join(projectDir, "telemetry.py"), traces: &tracingFile{Enabled: new(true)}, wantError: "project-relative"},
		{name: "parent component", configPath: "nested/../telemetry.py", traces: &tracingFile{Enabled: new(true)}, wantError: "project-relative"},
		{name: "wrong extension", configPath: "telemetry.txt", traces: &tracingFile{Enabled: new(true)}, wantError: "ending in .py"},
		{name: "missing file", configPath: "missing.py", traces: &tracingFile{Enabled: new(true)}, wantError: "file does not exist"},
		{name: "symlink escape", configPath: "outside.py", traces: &tracingFile{Enabled: new(true)}, wantError: "inside the project directory"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &configFile{Observability: &observabilityFile{Config: new(test.configPath), Traces: test.traces}}
			result := ValidateConfigFile(cfg, WithProjectDir(projectDir))
			if test.wantError == "" {
				require.False(t, result.HasErrors(), "errors: %v", result.Errors)
				return
			}
			require.ErrorContains(t, result.Err(), test.wantError)
		})
	}
}

func TestValidateConfigFileDeprecatedPythonPackages(t *testing.T) {
	cfg := &configFile{
		Build: &buildFile{
			PythonVersion: new("3.10"),
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
			PythonVersion: new("3.10"),
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
			GPU: new(true),
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
	model := new("registry.example.com/acme/my-model")

	tests := []struct {
		name    string
		image   *string
		model   *string
		weights []weightFile
		wantErr string // empty means expect no error
	}{
		{
			name:  "valid with two weights",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/base"}}}},
				{Name: "lora", Target: "/src/lora", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/lora"}}}},
			},
		},
		{
			name:  "valid with source",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model", Exclude: []string{"*.onnx"}}}}},
			},
		},
		{
			// Multi-source weights are valid: include/exclude
			// patterns are validated per-source, and the array
			// form must pass the same checks as the single-source
			// case.
			name:  "valid with multiple sources",
			model: model,
			weights: []weightFile{
				{Name: "merged", Target: "/src/weights/merged", Source: WeightSourceList{Items: []WeightSourceConfig{
					{URI: "hf://acme/base", Include: []string{"*.safetensors"}},
					{URI: "https://example.com/extras.bin"},
				}}},
			},
		},
		{
			// A bad pattern in any source surfaces with that
			// source's index, not the weight's.
			name:  "invalid pattern in second source",
			model: model,
			weights: []weightFile{
				{Name: "merged", Target: "/src/w", Source: WeightSourceList{Items: []WeightSourceConfig{
					{URI: "hf://acme/base"},
					{URI: "https://example.com/extras.bin", Include: []string{"!*.bin"}},
				}}},
			},
			wantErr: "negation patterns",
		},
		{
			name:    "missing source",
			model:   model,
			weights: []weightFile{{Name: "base", Target: "/src/weights"}},
			wantErr: "source is required",
		},
		{
			name: "weights without model",
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "weights require 'model' in cog.yaml",
		},
		{
			name:  "weights with image instead of model",
			image: new("registry.example.com/acme/my-model"),
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "weights require 'model', not 'image' — rename 'image' to 'model'",
		},
		{
			name:  "missing name",
			model: model,
			weights: []weightFile{
				{Name: "", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "name is required",
		},
		{
			name:  "uppercase name",
			model: model,
			weights: []weightFile{
				{Name: "MyModel", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "name with spaces",
			model: model,
			weights: []weightFile{
				{Name: "my model", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "name starting with hyphen",
			model: model,
			weights: []weightFile{
				{Name: "-base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "must contain only lowercase",
		},
		{
			name:  "valid name with separators",
			model: model,
			weights: []weightFile{
				{Name: "z-image.turbo_v1", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
		},
		{
			name:  "duplicate name",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
				{Name: "base", Target: "/src/other", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "duplicate weight name",
		},
		{
			name:  "missing target",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "target is required",
		},
		{
			name:  "relative target",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "target must be an absolute path",
		},
		{
			name:  "duplicate target",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
				{Name: "lora", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "duplicate weight target",
		},
		{
			name:  "overlapping targets parent then child",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
				{Name: "lora", Target: "/src/weights/lora", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/lora"}}}},
			},
			wantErr: "target overlaps with",
		},
		{
			name:  "overlapping targets child then parent",
			model: model,
			weights: []weightFile{
				{Name: "lora", Target: "/src/weights/lora", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/lora"}}}},
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/model"}}}},
			},
			wantErr: "target overlaps with",
		},
		{
			name:  "disjoint targets no false positive",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/base"}}}},
				{Name: "lora", Target: "/src/weights2", Source: WeightSourceList{Items: []WeightSourceConfig{{URI: "hf://acme/lora"}}}},
			},
		},
		{
			name:  "valid include and exclude patterns",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{
					URI:     "hf://acme/model",
					Include: []string{"*.safetensors", "*.json"},
					Exclude: []string{"*.onnx", "*.bin"},
				}}}},
			},
		},
		{
			name:  "empty string in include pattern",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{
					URI:     "hf://acme/model",
					Include: []string{"*.safetensors", ""},
				}}}},
			},
			wantErr: "pattern must not be empty",
		},
		{
			name:  "empty string in exclude pattern",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{
					URI:     "hf://acme/model",
					Exclude: []string{""},
				}}}},
			},
			wantErr: "pattern must not be empty",
		},
		{
			name:  "negation pattern in include",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{
					URI:     "hf://acme/model",
					Include: []string{"!*.bin"},
				}}}},
			},
			wantErr: "negation patterns",
		},
		{
			name:  "negation pattern in exclude",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{
					URI:     "hf://acme/model",
					Exclude: []string{"!*.safetensors"},
				}}}},
			},
			wantErr: "negation patterns",
		},
		{
			name:  "whitespace-only pattern rejected after trim",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{
					URI:     "hf://acme/model",
					Include: []string{"  "},
				}}}},
			},
			wantErr: "pattern must not be empty",
		},
		{
			name:  "backslash in pattern rejected",
			model: model,
			weights: []weightFile{
				{Name: "base", Target: "/src/weights", Source: WeightSourceList{Items: []WeightSourceConfig{{
					URI:     "hf://acme/model",
					Exclude: []string{`onnx\*.bin`},
				}}}},
			},
			wantErr: "must use forward slashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &configFile{
				Build:   &buildFile{PythonVersion: new("3.12")},
				Image:   tt.image,
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

func TestValidateModel(t *testing.T) {
	tests := []struct {
		name    string
		image   *string
		model   *string
		wantErr string // empty means expect no error
	}{
		{
			name:  "valid bare repo with registry",
			model: new("registry.example.com/acme/my-model"),
		},
		{
			name:  "valid bare repo without registry",
			model: new("acme/my-model"),
		},
		{
			name:  "valid single-segment repo",
			model: new("my-model"),
		},
		{
			name:  "valid host:port repo",
			model: new("localhost:5000/acme/my-model"),
		},
		{
			name:    "model with tag rejected",
			model:   new("registry.example.com/acme/my-model:v1"),
			wantErr: "must be a bare repository",
		},
		{
			name:    "model with digest rejected",
			model:   new("registry.example.com/acme/my-model@sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
			wantErr: "must be a bare repository",
		},
		{
			name:    "model and image both set",
			model:   new("registry.example.com/acme/my-model"),
			image:   new("registry.example.com/acme/my-image"),
			wantErr: "'model' and 'image' cannot both be set",
		},
		{
			name:    "model with invalid characters",
			model:   new("Registry.Example.Com/ACME/Model"),
			wantErr: "invalid repository",
		},
		{
			name: "no model and no image is fine",
		},
		{
			name:  "image only is fine",
			image: new("registry.example.com/acme/my-image"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &configFile{
				Build: &buildFile{PythonVersion: new("3.12")},
				Image: tt.image,
				Model: tt.model,
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
