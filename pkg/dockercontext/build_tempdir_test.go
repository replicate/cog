package dockercontext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildTempDir(t *testing.T) {
	tmpDir := t.TempDir()
	buildDir, err := BuildTempDir(tmpDir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmpDir, ".cog/build"), buildDir)

	// Directory should exist
	info, err := os.Stat(buildDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestBuildTempDir_Stable(t *testing.T) {
	tmpDir := t.TempDir()
	dir1, err := BuildTempDir(tmpDir)
	require.NoError(t, err)
	dir2, err := BuildTempDir(tmpDir)
	require.NoError(t, err)
	require.Equal(t, dir1, dir2, "BuildTempDir should return the same path on repeated calls")
}
