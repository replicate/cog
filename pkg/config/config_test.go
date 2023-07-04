package config

import (
	"encoding/json"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestPythonPackagesAndRequirementsCantBeUsedTogether(t *testing.T) {
	config := &Config{
		Build: &Build{
			PythonVersion: "3.8",
			PythonPackages: []string{
				"replicate==1.0.0",
			},
			PythonRequirements: "requirements.txt",
		},
	}
	err := config.ValidateAndComplete("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Only one of python_packages or python_requirements can be set in your cog.yaml, not both")
}

func TestPythonRequirementsResolvesPythonPackagesAndCudaVersions(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(path.Join(tmpDir, "requirements.txt"), []byte(`torch==1.7.1
torchvision==0.8.2
torchaudio==0.7.2
foo==1.0.0`), 0o644)
	require.NoError(t, err)

	config := &Config{
		Build: &Build{
			GPU:                true,
			PythonVersion:      "3.8",
			PythonRequirements: "requirements.txt",
		},
	}
	err = config.ValidateAndComplete(tmpDir)
	require.NoError(t, err)
	require.Equal(t, "11.0.3", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)

	requirements, err := config.PythonRequirementsForArch("", "")
	require.NoError(t, err)
	expected := `--find-links https://download.pytorch.org/whl/torch_stable.html
torch==1.7.1+cu110
torchvision==0.8.2+cu110
torchaudio==0.7.2
foo==1.0.0`
	require.Equal(t, expected, requirements)
}

func TestPythonRequirementsResolvesPythonPackagesAndCudaVersionsWithExtraIndexURL(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(path.Join(tmpDir, "requirements.txt"), []byte(`torch==1.12.1
torchvision==0.13.1
torchaudio==0.12.1
foo==1.0.0`), 0o644)
	require.NoError(t, err)

	config := &Config{
		Build: &Build{
			GPU:                true,
			PythonVersion:      "3.8",
			PythonRequirements: "requirements.txt",
		},
	}
	err = config.ValidateAndComplete(tmpDir)
	require.NoError(t, err)
	require.Equal(t, "11.6.2", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)

	requirements, err := config.PythonRequirementsForArch("", "")
	require.NoError(t, err)
	expected := `--extra-index-url https://download.pytorch.org/whl/cu116
torch==1.12.1+cu116
torchvision==0.13.1+cu116
torchaudio==0.12.1
foo==1.0.0`
	require.Equal(t, expected, requirements)
}

func TestPythonRequirementsWorksWithLinesCogCannotParse(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(path.Join(tmpDir, "requirements.txt"), []byte(`foo==1.0.0
# a torch which already has a version
torch==1.7.1+cu110
# complex requirements
fastapi>=0.6,<1
flask>0.4
# comments!
# blank lines!

# arguments
-f http://example.com`), 0o644)
	require.NoError(t, err)

	config := &Config{
		Build: &Build{
			GPU:                true,
			PythonVersion:      "3.8",
			PythonRequirements: "requirements.txt",
		},
	}
	err = config.ValidateAndComplete(tmpDir)
	require.NoError(t, err)

	requirements, err := config.PythonRequirementsForArch("", "")
	require.NoError(t, err)
	expected := `foo==1.0.0
# a torch which already has a version
torch==1.7.1+cu110
# complex requirements
fastapi>=0.6,<1
flask>0.4
# comments!
# blank lines!

# arguments
-f http://example.com`
	require.Equal(t, expected, requirements)

}

func TestValidateAndCompleteCUDAForAllTF(t *testing.T) {
	for _, compat := range TFCompatibilityMatrix {
		config := &Config{
			Build: &Build{
				GPU:           true,
				PythonVersion: "3.8",
				PythonPackages: []string{
					"tensorflow==" + compat.TF,
				},
			},
		}

		err := config.ValidateAndComplete("")
		require.NoError(t, err)
		require.Equal(t, compat.CUDA, config.Build.CUDA)
		require.Equal(t, compat.CuDNN, config.Build.CuDNN)
	}
}

func TestValidateAndCompleteCUDAForAllTorch(t *testing.T) {
	for _, compat := range TorchCompatibilityMatrix {
		config := &Config{
			Build: &Build{
				GPU:           compat.CUDA != nil,
				PythonVersion: "3.8",
				PythonPackages: []string{
					"torch==" + compat.TorchVersion(),
				},
			},
		}

		err := config.ValidateAndComplete("")
		require.NoError(t, err)
		require.NotEqual(t, "", config.Build.CUDA)
		require.NotEqual(t, "", config.Build.CuDNN)
	}
}

func TestValidateAndCompleteCUDAForSelectedTorch(t *testing.T) {
	for _, tt := range []struct {
		torch string
		cuda  string
		cuDNN string
	}{
		{"2.0.1", "11.8.0", "8"},
		{"1.8.0", "11.1.1", "8"},
		{"1.7.0", "11.0.3", "8"},
	} {
		config := &Config{
			Build: &Build{
				GPU:           true,
				PythonVersion: "3.8",
				PythonPackages: []string{
					"torch==" + tt.torch,
				},
			},
		}
		err := config.ValidateAndComplete("")
		require.NoError(t, err)
		require.Equal(t, tt.cuda, config.Build.CUDA)
		require.Equal(t, tt.cuDNN, config.Build.CuDNN)
	}
}

func TestUnsupportedTorch(t *testing.T) {
	// Ensure version is not known by Cog
	cudas, err := cudasFromTorch("0.4.1")
	require.NoError(t, err)
	require.Empty(t, cudas)

	// Unknown versions require cuda
	config := &Config{
		Build: &Build{
			GPU:           true,
			PythonVersion: "3.8",
			PythonPackages: []string{
				"torch==0.4.1",
			},
		},
	}
	err = config.ValidateAndComplete("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Cog doesn't know what CUDA version is compatible with torch==0.4.1.")

	config = &Config{
		Build: &Build{
			GPU:           true,
			CUDA:          "11.8",
			PythonVersion: "3.10",
			PythonPackages: []string{
				"torch==0.4.1",
			},
		},
	}
	err = config.ValidateAndComplete("")
	require.NoError(t, err)
	require.Equal(t, "11.8", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)
}

func TestUnsupportedTensorflow(t *testing.T) {
	// Ensure version is not known by Cog
	cuda, cudnn, err := cudaFromTF("0.4.1")
	require.NoError(t, err)
	require.Equal(t, cuda, "")
	require.Equal(t, cudnn, "")

	// Unknown versions require cuda
	config := &Config{
		Build: &Build{
			GPU:           true,
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==0.4.1",
			},
		},
	}
	err = config.ValidateAndComplete("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Cog doesn't know what CUDA version is compatible with tensorflow==0.4.1.")

	config = &Config{
		Build: &Build{
			GPU:           true,
			CUDA:          "11.8",
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==0.4.1",
			},
		},
	}
	err = config.ValidateAndComplete("")
	require.NoError(t, err)
	require.Equal(t, "11.8", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)
}

func TestPythonPackagesForArchTorchGPU(t *testing.T) {
	config := &Config{
		Build: &Build{
			GPU:           true,
			PythonVersion: "3.8",
			PythonPackages: []string{
				"torch==1.7.1",
				"torchvision==0.8.2",
				"torchaudio==0.7.2",
				"foo==1.0.0",
			},
			CUDA: "11.8",
		},
	}
	err := config.ValidateAndComplete("")
	require.NoError(t, err)
	require.Equal(t, "11.8", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)

	requirements, err := config.PythonRequirementsForArch("", "")
	require.NoError(t, err)
	expected := `--find-links https://download.pytorch.org/whl/torch_stable.html
torch==1.7.1+cu110
torchvision==0.8.2+cu110
torchaudio==0.7.2
foo==1.0.0`
	require.Equal(t, expected, requirements)
}

func TestPythonPackagesForArchTorchCPU(t *testing.T) {
	config := &Config{
		Build: &Build{
			GPU:           false,
			PythonVersion: "3.8",
			PythonPackages: []string{
				"torch==1.7.1",
				"torchvision==0.8.2",
				"torchaudio==0.7.2",
				"foo==1.0.0",
			},
			CUDA: "11.8",
		},
	}
	err := config.ValidateAndComplete("")
	require.NoError(t, err)

	requirements, err := config.PythonRequirementsForArch("", "")
	require.NoError(t, err)
	expected := `torch==1.7.1
torchvision==0.8.2
torchaudio==0.7.2
foo==1.0.0`
	require.Equal(t, expected, requirements)
}

func TestPythonPackagesForArchTensorflowGPU(t *testing.T) {
	config := &Config{
		Build: &Build{
			GPU:           true,
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==2.12.0",
				"foo==1.0.0",
			},
			CUDA: "11.8",
		},
	}
	err := config.ValidateAndComplete("")
	require.NoError(t, err)
	require.Equal(t, "11.8", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)

	// tensorflow and tensorflow-gpu have been the same package since TensorFlow 2.1, released in September 2019.
	// Although the checksums differ due to metadata,
	// they were built in the same way and both provide GPU support via Nvidia CUDA.
	// As of December 2022, tensorflow-gpu has been removed and has been replaced with
	// this new, empty package that generates an error upon installation.
	requirements, err := config.PythonRequirementsForArch("", "")
	require.NoError(t, err)
	expected := `tensorflow==2.12.0
foo==1.0.0`
	require.Equal(t, expected, requirements)
	require.NotContains(t, requirements, "tensorflow_gpu")
}

func TestCUDABaseImageTag(t *testing.T) {
	config := &Config{
		Build: &Build{
			GPU:           true,
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==2.12.0",
			},
		},
	}

	err := config.ValidateAndComplete("")
	require.NoError(t, err)

	imageTag, err := config.CUDABaseImageTag()
	require.NoError(t, err)
	require.Equal(t, "nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04", imageTag)
}

func TestBuildRunItemStringYAML(t *testing.T) {
	type BuildWrapper struct {
		Build *Build `yaml:"build"`
	}

	var buildWrapper BuildWrapper

	yamlString := `
build:
  run:
    - "echo 'Hello, World!'"
`

	err := yaml.Unmarshal([]byte(yamlString), &buildWrapper)
	require.NoError(t, err)
	require.NotNil(t, buildWrapper.Build)
	require.Len(t, buildWrapper.Build.Run, 1)
	require.Equal(t, "echo 'Hello, World!'", buildWrapper.Build.Run[0].Command)
}

func TestBuildRunItemStringJSON(t *testing.T) {
	type BuildWrapper struct {
		Build *Build `json:"build"`
	}

	var buildWrapper BuildWrapper

	jsonString := `{
	"build": {
		"run": [
			"echo 'Hello, World!'"
		]
	}
}`

	err := json.Unmarshal([]byte(jsonString), &buildWrapper)
	require.NoError(t, err)
	require.NotNil(t, buildWrapper.Build)
	require.Len(t, buildWrapper.Build.Run, 1)
	require.Equal(t, "echo 'Hello, World!'", buildWrapper.Build.Run[0].Command)
}

func TestBuildRunItemDictYAML(t *testing.T) {
	type BuildWrapper struct {
		Build *Build `yaml:"build"`
	}

	var buildWrapper BuildWrapper

	yamlString := `
build:
  run:
    - command: "echo 'Hello, World!'"
      mounts:
        - type: bind
          id: my-volume
          target: /mnt/data
`

	err := yaml.Unmarshal([]byte(yamlString), &buildWrapper)
	require.NoError(t, err)
	require.NotNil(t, buildWrapper.Build)
	require.Len(t, buildWrapper.Build.Run, 1)
	require.Equal(t, "echo 'Hello, World!'", buildWrapper.Build.Run[0].Command)
	require.Len(t, buildWrapper.Build.Run[0].Mounts, 1)
	require.Equal(t, "bind", buildWrapper.Build.Run[0].Mounts[0].Type)
	require.Equal(t, "my-volume", buildWrapper.Build.Run[0].Mounts[0].ID)
	require.Equal(t, "/mnt/data", buildWrapper.Build.Run[0].Mounts[0].Target)
}

func TestBuildRunItemDictJSON(t *testing.T) {
	type BuildWrapper struct {
		Build *Build `json:"build"`
	}

	var buildWrapper BuildWrapper

	jsonString := `{
	"build": {
		"run": [
			{
				"command": "echo 'Hello, World!'",
				"mounts": [
					{
						"type": "bind",
						"id": "my-volume",
						"target": "/mnt/data"
					}
				]
			}
		]
	}
}`

	err := json.Unmarshal([]byte(jsonString), &buildWrapper)
	require.NoError(t, err)
	require.NotNil(t, buildWrapper.Build)
	require.Len(t, buildWrapper.Build.Run, 1)
	require.Equal(t, "echo 'Hello, World!'", buildWrapper.Build.Run[0].Command)
	require.Len(t, buildWrapper.Build.Run[0].Mounts, 1)
	require.Equal(t, "bind", buildWrapper.Build.Run[0].Mounts[0].Type)
	require.Equal(t, "my-volume", buildWrapper.Build.Run[0].Mounts[0].ID)
	require.Equal(t, "/mnt/data", buildWrapper.Build.Run[0].Mounts[0].Target)
}

func TestBlankBuild(t *testing.T) {
	// Naively, this turns into nil, so make sure it's a real build object
	config, err := FromYAML([]byte(`build:`))
	require.NoError(t, err)
	require.NotNil(t, config.Build)
	require.Equal(t, false, config.Build.GPU)
}
