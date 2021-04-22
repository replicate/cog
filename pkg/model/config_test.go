package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAndCompleteCUDAForAllTF(t *testing.T) {
	for _, compat := range TFCompatibilityMatrix {
		config := &Config{
			Environment: &Environment{
				PythonVersion: "3.8",
				PythonPackages: []string{
					"tensorflow==" + compat.TF,
				},
			},
		}

		err := config.validateAndCompleteCUDA()
		require.NoError(t, err)
		require.Equal(t, compat.CUDA, config.Environment.CUDA)
		require.Equal(t, compat.CuDNN, config.Environment.CuDNN)
	}
}

func TestValidateAndCompleteCUDAForAllTorch(t *testing.T) {
	// test that all torch versions fill out cuda
	for _, compat := range TorchCompatibilityMatrix {
		config := &Config{
			Environment: &Environment{
				PythonVersion: "3.8",
				PythonPackages: []string{
					"torch==" + compat.TorchVersion(),
				},
			},
		}

		err := config.validateAndCompleteCUDA()
		require.NoError(t, err)
		require.NotEqual(t, "", config.Environment.CUDA)
		require.NotEqual(t, "", config.Environment.CuDNN)
	}

	// test correctness for a subset
	for _, tt := range []struct {
		torch string
		cuda  string
		cuDNN string
	}{
		{"1.8.0", "11.1", "8"},
		{"1.7.0", "11.0", "8"},
		{"1.5.1", "10.2", "8"},
	} {
		config := &Config{
			Environment: &Environment{
				PythonVersion: "3.8",
				PythonPackages: []string{
					"torch==" + tt.torch,
				},
			},
		}
		err := config.validateAndCompleteCUDA()
		require.NoError(t, err)
		require.Equal(t, tt.cuda, config.Environment.CUDA)
		require.Equal(t, tt.cuDNN, config.Environment.CuDNN)
	}
}

func TestPythonPackagesForArchTorchGPU(t *testing.T) {
	config := &Config{
		Environment: &Environment{
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
	require.Equal(t, "10.1", config.Environment.CUDA)
	require.Equal(t, "8", config.Environment.CuDNN)

	packages, indexURLs, err := config.PythonPackagesForArch("gpu", "", "")
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
		Environment: &Environment{
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
	require.Equal(t, "10.1", config.Environment.CUDA)
	require.Equal(t, "8", config.Environment.CuDNN)

	packages, indexURLs, err := config.PythonPackagesForArch("gpu", "", "")
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

func TestPythonPackagesForArchTensorflowGPU(t *testing.T) {
	config := &Config{
		Environment: &Environment{
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
	require.Equal(t, "10.0", config.Environment.CUDA)
	require.Equal(t, "7", config.Environment.CuDNN)

	packages, indexURLs, err := config.PythonPackagesForArch("gpu", "", "")
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
		Environment: &Environment{
			PythonVersion: "3.8",
			PythonPackages: []string{
				"tensorflow==1.8.0",
			},
		},
	}

	err := config.validateAndCompleteCUDA()
	require.NoError(t, err)

	imageTag, err := config.CUDABaseImageTag()
	require.NoError(t, err)
	require.Equal(t, "nvidia/cuda:9.0-cudnn7-devel-ubuntu16.04", imageTag)
}
