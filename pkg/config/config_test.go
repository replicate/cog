package config

import (
	"testing"

	"github.com/stretchr/testify/require"
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
	err := config.ValidateAndCompleteConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "Only one of python_packages or python_requirements can be set in your cog.yaml, not both")
}

func TestValidateAndCompleteCUDAForAllTF(t *testing.T) {
	for _, compat := range TFCompatibilityMatrix {
		config := &Config{
			Build: &Build{
				PythonVersion: "3.8",
				PythonPackages: []string{
					"tensorflow==" + compat.TF,
				},
			},
		}

		err := config.validateAndCompleteCUDA()
		require.NoError(t, err)
		require.Equal(t, compat.CUDA, config.Build.CUDA)
		require.Equal(t, compat.CuDNN, config.Build.CuDNN)
	}
}

func TestValidateAndCompleteCUDAForAllTorch(t *testing.T) {
	// test that all torch versions fill out cuda
	for _, compat := range TorchCompatibilityMatrix {
		config := &Config{
			Build: &Build{
				GPU:           true,
				PythonVersion: "3.8",
				PythonPackages: []string{
					"torch==" + compat.TorchVersion(),
				},
			},
		}

		err := config.validateAndCompleteCUDA()
		require.NoError(t, err)
		require.NotEqual(t, "", config.Build.CUDA)
		require.NotEqual(t, "", config.Build.CuDNN)
	}

	// test correctness for a subset
	for _, tt := range []struct {
		torch string
		cuda  string
		cuDNN string
	}{
		{"1.8.0", "11.1.1", "8"},
		{"1.7.0", "11.0.3", "8"},
		{"1.5.1", "10.2", "8"},
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
		err := config.validateAndCompleteCUDA()
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
	err = config.validateAndCompleteCUDA()
	require.Error(t, err)
	require.Contains(t, err.Error(), "Cog couldn't automatically determine a CUDA version for torch==0.4.1.")

	config = &Config{
		Build: &Build{
			GPU:           true,
			CUDA:          "9.1",
			PythonVersion: "3.8",
			PythonPackages: []string{
				"torch==0.4.1",
			},
		},
	}
	err = config.validateAndCompleteCUDA()
	require.NoError(t, err)
	require.Equal(t, "9.1", config.Build.CUDA)
	require.Equal(t, "7", config.Build.CuDNN)

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
	err = config.validateAndCompleteCUDA()
	require.Error(t, err)
	require.Contains(t, err.Error(), "Cog couldn't automatically determine a CUDA version for tensorflow==0.4.1.")

	config = &Config{
		Build: &Build{
			GPU:           true,
			CUDA:          "9.1",
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==0.4.1",
			},
		},
	}
	err = config.validateAndCompleteCUDA()
	require.NoError(t, err)
	require.Equal(t, "9.1", config.Build.CUDA)
	require.Equal(t, "7", config.Build.CuDNN)

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
			CUDA: "10.1",
		},
	}
	err := config.validateAndCompleteCUDA()
	require.NoError(t, err)
	require.Equal(t, "10.1", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)

	packages, indexURLs, err := config.PythonPackagesForArch("", "")
	require.NoError(t, err)
	expectedPackages := []string{
		"torch==1.7.1+cu101",
		"torchvision==0.8.2+cu101",
		"torchaudio==0.7.2",
		"foo==1.0.0",
	}
	expectedIndexURLs := []string{"https://download.pytorch.org/whl/torch_stable.html"}
	require.Equal(t, expectedPackages, packages)
	require.Equal(t, expectedIndexURLs, indexURLs)
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
			CUDA: "10.1",
		},
	}
	err := config.validateAndCompleteCUDA()
	require.NoError(t, err)
	require.Equal(t, "10.1", config.Build.CUDA)
	require.Equal(t, "8", config.Build.CuDNN)

	packages, indexURLs, err := config.PythonPackagesForArch("", "")
	require.NoError(t, err)
	expectedPackages := []string{
		"torch==1.7.1+cpu",
		"torchvision==0.8.2+cpu",
		"torchaudio==0.7.2",
		"foo==1.0.0",
	}
	expectedIndexURLs := []string{"https://download.pytorch.org/whl/torch_stable.html"}
	require.Equal(t, expectedPackages, packages)
	require.Equal(t, expectedIndexURLs, indexURLs)
}

func TestPythonPackagesForArchTensorflowGPU(t *testing.T) {
	config := &Config{
		Build: &Build{
			GPU:           true,
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==1.15.0",
				"foo==1.0.0",
			},
			CUDA: "10.0",
		},
	}
	err := config.validateAndCompleteCUDA()
	require.NoError(t, err)
	require.Equal(t, "10.0", config.Build.CUDA)
	require.Equal(t, "7", config.Build.CuDNN)

	packages, indexURLs, err := config.PythonPackagesForArch("", "")
	require.NoError(t, err)
	expectedPackages := []string{
		"tensorflow_gpu==1.15.0",
		"foo==1.0.0",
	}
	expectedIndexURLs := []string{}
	require.Equal(t, expectedPackages, packages)
	require.Equal(t, expectedIndexURLs, indexURLs)
}

func TestCUDABaseImageTag(t *testing.T) {
	config := &Config{
		Build: &Build{
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==1.13.1",
			},
		},
	}

	err := config.validateAndCompleteCUDA()
	require.NoError(t, err)

	imageTag, err := config.CUDABaseImageTag()
	require.NoError(t, err)
	require.Equal(t, "nvidia/cuda:10.0-cudnn7-devel-ubuntu18.04", imageTag)
}

func TestBlankBuild(t *testing.T) {
	// Naively, this turns into nil, so make sure it's a real build object
	config, err := FromYAML([]byte(`build:`))
	require.NoError(t, err)
	require.NotNil(t, config.Build)
	require.Equal(t, false, config.Build.GPU)

}
