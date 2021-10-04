package cli

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "cog-init-test")
	require.NoError(t, err)

	require.NoError(t, os.Chdir(dir))

	err = initCommand([]string{})
	require.NoError(t, err)

	require.FileExists(t, path.Join(dir, "cog.yaml"))
	require.FileExists(t, path.Join(dir, "predict.py"))
}
