package dockerfile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	generator, err := NewBaseImageGenerator(
		"12.1",
		"3.8",
		"2.1.0",
	)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfile()
	require.NoError(t, err)
	require.True(t, strings.Contains(dockerfile, "FROM nvidia/cuda:12.1.1-cudnn8-devel-ubuntu22.04"))
}

func TestBaseImageNameWithVersionModifier(t *testing.T) {
	actual := BaseImageName("12.1", "3.8", "2.0.1+cu118")
	require.Equal(t, "r8.im/cog-base:cuda12.1-python3.8-torch2.0.1", actual)
}

func TestBaseImageConfigurationExists(t *testing.T) {
	exists, _, _, torchVersion := BaseImageConfigurationExists("12.1", "3.9", "2.3")
	require.True(t, exists)
	require.Equal(t, "2.3.1", torchVersion)
}

func TestBaseImageConfigurationExistsNoTorch(t *testing.T) {
	exists, _, _, _ := BaseImageConfigurationExists("", "3.12", "")
	require.True(t, exists)
}

func TestBaseImageConfigurationExistsNoCUDA(t *testing.T) {
	exists, _, _, torchVersion := BaseImageConfigurationExists("", "3.8", "2.1")
	require.True(t, exists)
	require.Equal(t, "2.1.2", torchVersion)
}

func TestIsVersionCompatible(t *testing.T) {
	compatible := isVersionCompatible("2.3.1+cu121", "2.3")
	require.True(t, compatible)
}

func TestPythonPackages(t *testing.T) {
	generator, err := NewBaseImageGenerator("12.1", "3.9", "2.1.0")
	require.NoError(t, err)
	pkgs := generator.pythonPackages()
	require.Truef(t, reflect.DeepEqual(pkgs, []string{
		"torch==2.1.0",
		"opencv-python==4.10.0.84",
		"torchvision==0.16.0",
		"torchaudio==2.1.0",
	}), "expected %v", pkgs)
}
