package docker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateAptTarball(t *testing.T) {
	dir := t.TempDir()
	command := NewMockCommand()
	tarball, err := CreateAptTarball(dir, command, []string{}...)
	require.NoError(t, err)
	require.Equal(t, "", tarball)
}
