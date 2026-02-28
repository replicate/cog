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
				"libgl1-mesa-glx",
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
				"libgl1-mesa-glx",
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

// ptr returns a pointer to the given value.
func ptr[T any](v T) *T { return &v }
