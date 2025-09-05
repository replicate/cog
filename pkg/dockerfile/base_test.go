package dockerfile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/registry/registrytest"
)

func TestBaseImageName(t *testing.T) {
	for _, tt := range []struct {
		cuda     string
		python   string
		torch    string
		expected string
	}{
		{"", "3.8", "",
			"r8.im/cog-base:python3.8"},
		{"", "3.8", "2.1",
			"r8.im/cog-base:python3.8-torch2.1.2"},
		{"12.1", "3.8", "",
			"r8.im/cog-base:cuda12.1-python3.8"},
		{"12.1", "3.8", "2.1",
			"r8.im/cog-base:cuda12.1-python3.8-torch2.1.2"},
		{"12.1", "3.8", "2.1",
			"r8.im/cog-base:cuda12.1-python3.8-torch2.1.2"},
	} {
		actual := BaseImageName(tt.cuda, tt.python, tt.torch)
		require.Equal(t, tt.expected, actual)
	}
}

func TestGenerateDockerfile(t *testing.T) {
	cudaVersion := "12.1"
	pythonVersion := "3.8"
	torchVersion := "2.1.0"
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName(cudaVersion, pythonVersion, torchVersion))
	command := dockertest.NewMockCommand()
	generator, err := NewBaseImageGenerator(
		t.Context(),
		client,
		cudaVersion,
		pythonVersion,
		torchVersion,
		command,
		false,
	)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfile(t.Context())
	require.NoError(t, err)
	require.True(t, strings.Contains(dockerfile, "FROM nvidia/cuda:12.1.1-cudnn8-devel-ubuntu22.04"))
}

func TestBaseImageNameWithVersionModifier(t *testing.T) {
	actual := BaseImageName("11.8", "3.8", "2.0.1+cu118")
	require.Equal(t, "r8.im/cog-base:cuda11.8-python3.8-torch2.0.1", actual)
}

func TestBaseImageConfigurationExists(t *testing.T) {
	cudaVersion := "12.1"
	pythonVersion := "3.9"
	torchVersion := "2.3"
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName(cudaVersion, pythonVersion, torchVersion))
	exists, _, _, torchVersion, err := BaseImageConfigurationExists(t.Context(), client, cudaVersion, pythonVersion, torchVersion, false)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "2.3.1", torchVersion)
}

func TestBaseImageConfigurationExistsNoTorch(t *testing.T) {
	cudaVersion := ""
	pythonVersion := "3.12"
	torchVersion := ""
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName(cudaVersion, pythonVersion, torchVersion))
	exists, _, _, _, err := BaseImageConfigurationExists(t.Context(), client, cudaVersion, pythonVersion, torchVersion, false)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestBaseImageConfigurationExistsNoCUDA(t *testing.T) {
	cudaVersion := ""
	pythonVersion := "3.8"
	torchVersion := "2.1"
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName(cudaVersion, pythonVersion, torchVersion))
	exists, _, _, torchVersion, err := BaseImageConfigurationExists(t.Context(), client, cudaVersion, pythonVersion, torchVersion, false)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "2.1.2", torchVersion)
}

func TestIsVersionCompatible(t *testing.T) {
	compatible := isVersionCompatible("2.3.1+cu121", "2.3")
	require.True(t, compatible)
}

func TestPythonPackages(t *testing.T) {
	cudaVersion := "12.1"
	pythonVersion := "3.9"
	torchVersion := "2.1.0"
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName(cudaVersion, pythonVersion, torchVersion))
	generator, err := NewBaseImageGenerator(t.Context(), client, cudaVersion, pythonVersion, torchVersion, command, false)
	require.NoError(t, err)
	pkgs := generator.pythonPackages()
	require.Truef(t, reflect.DeepEqual(pkgs, []string{
		"torch==" + torchVersion,
		"opencv-python==4.12.0.88",
		"torchvision==0.16.0",
		"torchaudio==2.1.0",
	}), "expected %v", pkgs)
}

func TestInvalidBaseImage(t *testing.T) {
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	_, err := NewBaseImageGenerator(t.Context(), client, "12.78", "3.9", "2.1.0", command, false)
	require.Error(t, err)
}

func TestBaseImageConfigurationNoTorchPythonVersionDoesNotExist(t *testing.T) {
	client := registrytest.NewMockRegistryClient()
	exists, _, _, _, err := BaseImageConfigurationExists(t.Context(), client, "", "3.99", "", false)
	require.NoError(t, err)
	require.False(t, exists)
}
