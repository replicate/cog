package mime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtensionByType(t *testing.T) {
	require.Equal(t, ".txt", ExtensionByType("text/plain"))
	require.Equal(t, ".jpg", ExtensionByType("image/jpeg"))
	require.Equal(t, ".png", ExtensionByType("image/png"))
	require.Equal(t, ".obj", ExtensionByType("model/obj"))
	require.Equal(t, ".json", ExtensionByType("application/json"))
	require.Equal(t, "", ExtensionByType("asdfasdf"))
}

func TestTypeByExtension(t *testing.T) {
	require.Equal(t, "text/plain", TypeByExtension(".txt"))
	require.Equal(t, "image/jpeg", TypeByExtension(".jpg"))
	require.Equal(t, "image/png", TypeByExtension(".png"))
	require.Equal(t, "model/obj", TypeByExtension(".obj"))
	require.Equal(t, "application/json", TypeByExtension(".json"))
	require.Equal(t, "application/octet-stream", TypeByExtension(".asdfasdf"))
}
