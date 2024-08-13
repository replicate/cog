package dockerfile

import (
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

func TestBaseImageConfigurations(t *testing.T) {
	actual := BaseImageConfigurations()
	foundTorch1_13 := false
	for _, config := range actual {
		if config.TorchVersion == "1.13" {
			foundTorch1_13 = true
			break
		}
	}
	require.True(t, foundTorch1_13)
}
