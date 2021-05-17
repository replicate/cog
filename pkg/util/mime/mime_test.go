package mime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtensionByType(t *testing.T) {
	require.Equal(t, ".txt", ExtensionByType("text/plain"))
	require.Equal(t, ".jpg", ExtensionByType("image/jpeg"))
	require.Equal(t, ".png", ExtensionByType("image/png"))
	require.Equal(t, ".json", ExtensionByType("application/json"))
	require.Equal(t, "", ExtensionByType("asdfasdf"))
}
