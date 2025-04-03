package dockercontext

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildCogTempDir(t *testing.T) {
	tmpDir := t.TempDir()
	cogTmpDir, err := BuildCogTempDir(tmpDir, "weights")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tmpDir, ".cog/tmp/weights"), cogTmpDir)
}
