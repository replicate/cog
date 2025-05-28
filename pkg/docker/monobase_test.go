package docker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestCreateAptTarball(t *testing.T) {
	dir := t.TempDir()
	command := dockertest.NewMockCommand()
	tarball, err := CreateAptTarball(t.Context(), dir, command, []string{}...)
	require.NoError(t, err)
	require.Equal(t, "", tarball)
}

func TestCreateAptTarballWithPackages(t *testing.T) {
	dir := t.TempDir()
	command := dockertest.NewMockCommand()
	tarball, err := CreateAptTarball(t.Context(), dir, command, []string{"git"}...)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(tarball, "apt."))
}
