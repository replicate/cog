package dockerfile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWheelFilename(t *testing.T) {
	filename, err := WheelFilename()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(filename, "cog-"))
}
