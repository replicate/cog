package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfig(t *testing.T) {
	config := &Config{
		Build: &Build{
			GPU:           true,
			PythonVersion: "3.10",
			PythonPackages: []string{
				"tensorflow==1.15.0",
				"foo==1.0.0",
			},
			CUDA: "10.0",
		},
	}
	err := ValidateConfig(config, "1.0")
	require.NoError(t, err)
}

func TestValidateSuccess(t *testing.T) {
	config := `build:
  gpu: true
  system_packages:
    - "libgl1-mesa-glx"
    - "libglib2.0-0"
  python_version: "3.10"
  python_packages:
    - "torch==1.8.1"`

	err := Validate(config, "1.0")
	require.NoError(t, err)
}

func TestValidatePythonVersionNumerical(t *testing.T) {
	config := `build:
  gpu: true
  system_packages:
    - "libgl1-mesa-glx"
    - "libglib2.0-0"
  python_version: 3.10
  python_packages:
    - "torch==1.8.1"`

	err := Validate(config, "1.0")
	require.NoError(t, err)
}

func TestValidateBuildIsRequired(t *testing.T) {
	config := `buildd:
  gpu: true
  system_packages:
    - "libgl1-mesa-glx"
    - "libglib2.0-0"
  python_version: "3.10"
  python_packages:
    - "torch==1.8.1"`

	err := Validate(config, "1.0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Additional property buildd is not allowed")
}

func TestValidatePythonVersionIsRequired(t *testing.T) {
	config := `build:
  gpu: true
  python_versions: "3.10"
  system_packages:
    - "libgl1-mesa-glx"
    - "libglib2.0-0"
  python_packages:
    - "torch==1.8.1"`

	err := Validate(config, "1.0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Additional property python_versions is not allowed")
}

func TestValidateNullListsAllowed(t *testing.T) {
	config := `build:
  gpu: true
  python_version: "3.10"
  system_packages:
  python_packages:
  run:`

	err := Validate(config, "1.0")
	require.NoError(t, err)
}

func TestValidateOutputsPropertyFromTypeError(t *testing.T) {
	config := `build:
  gpu: true
  cuda: "11.8"
  python_version: "3.11"
  python_packages:
    - "torch==2.0.1"

predict: "predict.py:Predictor"
concurrency: 54`

	err := Validate(config, "1.0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "concurrency must be a mapping.")
}

func TestValidateWeightsWithName(t *testing.T) {
	config := `build:
  python_version: "3.12"
predict: "predict.py:Predictor"

weights:
  - name: model-v1
    source: file://./weights/model-v1.zip
    target: "/weights/model-v1"
  - name: model-v2
    source: file://./weights/model-v2.zip
    target: "/weights/model-v2"`

	err := Validate(config, "1.0")
	require.NoError(t, err)
}

func TestValidateWeightsWithoutName(t *testing.T) {
	config := `build:
  python_version: "3.12"
predict: "predict.py:Predictor"

weights:
  - source: file://./weights/model.zip
    target: "/weights/model"`

	err := Validate(config, "1.0")
	require.NoError(t, err)
}

func TestValidateWeightsWithInvalidProperty(t *testing.T) {
	config := `build:
  python_version: "3.12"
predict: "predict.py:Predictor"

weights:
  - name: model-v1
    source: file://./weights/model-v1.zip
    target: "/weights/model-v1"
    invalid_property: "should fail"`

	err := Validate(config, "1.0")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Additional property invalid_property is not allowed")
}
