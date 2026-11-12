package mime

import (
	"mime"
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

func TestTypeByExtensionMidi(t *testing.T) {
	require.Equal(t, "audio/midi", TypeByExtension(".midi"))
	require.Equal(t, ".midi", ExtensionByType("audio/midi"))
}

// A key that is not a single media type, such as two types joined by a space,
// would be returned by TypeByExtension and is rejected where the type is used.
func TestTypeToExtensionKeysAreMediaTypes(t *testing.T) {
	for typ := range typeToExtension {
		_, _, err := mime.ParseMediaType(typ)
		require.NoError(t, err, "key %q is not a valid media type", typ)
	}
	for ext, typ := range extensionToType {
		_, _, err := mime.ParseMediaType(typ)
		require.NoError(t, err, "extension %q maps to %q, which is not a valid media type", ext, typ)
	}
}
