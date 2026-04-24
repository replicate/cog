package paths

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightsStoreDir_COGCacheDir_Wins(t *testing.T) {
	t.Setenv("COG_CACHE_DIR", "/tmp/custom-cache")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-ignored")

	dir, err := WeightsStoreDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/tmp/custom-cache", "weights"), dir)
}

func TestWeightsStoreDir_XDGCacheHome_RespectedOnAllPlatforms(t *testing.T) {
	t.Setenv("COG_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg")

	dir, err := WeightsStoreDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/tmp/xdg", "cog", "weights"), dir)
}

// TestWeightsStoreDir_Defaults verifies the no-env default is
// $HOME/.cache/cog/weights on every platform — explicitly NOT
// $HOME/Library/Caches on macOS. Dev tools conventionally live under
// ~/.cache or ~/.<toolname>.
func TestWeightsStoreDir_Defaults(t *testing.T) {
	t.Setenv("COG_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir, err := WeightsStoreDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".cache", "cog", "weights"), dir)
}
