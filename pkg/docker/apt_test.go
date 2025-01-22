package docker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestCreateAptTarball(t *testing.T) {
	build := config.Build{}
	config := config.Config{
		Build: &build,
	}
	dir := t.TempDir()
	command := NewMockCommand()
	tarball, err := CreateAptTarball(&config, dir, command)
	require.NoError(t, err)
	require.Equal(t, "", tarball)
}
