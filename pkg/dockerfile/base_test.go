package dockerfile

import (
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
			"r8.im/cog-base:python3.8-torch2.1"},
		{"12.1", "3.8", "",
			"r8.im/cog-base:cuda12.1-python3.8"},
		{"12.1", "3.8", "2.1",
			"r8.im/cog-base:cuda12.1-python3.8-torch2.1"},
		{"12.1", "3.8", "2.1",
			"r8.im/cog-base:cuda12.1-python3.8-torch2.1"},
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
