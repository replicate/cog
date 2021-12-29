package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsExecutable(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "test-files")
	require.NoError(t, err)
	path := filepath.Join(dir, "test-file")
	err = os.WriteFile(path, []byte{}, 0o644)
	require.NoError(t, err)

	require.False(t, IsExecutable(path))
	require.NoError(t, os.Chmod(path, 0o744))
	require.True(t, IsExecutable(path))
}
