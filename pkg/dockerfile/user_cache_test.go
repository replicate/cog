package dockerfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserCache(t *testing.T) {
	userCache, err := UserCache()
	require.NoError(t, err)
	lastDirectory := filepath.Base(userCache)
	require.Equal(t, COG_CACHE_FOLDER, lastDirectory)
}
