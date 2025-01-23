package docker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestCreateAptTarball(t *testing.T) {
	dir := t.TempDir()
	command := dockertest.NewMockCommand()
	tarball, err := CreateAptTarball(dir, command, []string{}...)
	require.NoError(t, err)
	require.Equal(t, "", tarball)
}
