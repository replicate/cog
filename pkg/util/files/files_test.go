package files

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteBadlyFormattedBase64DataURI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-file")
	_, err := WriteDataURLToFile("data:None;base64,SGVsbG8gVGhlcmU=", path)
	require.NoError(t, err)
}

func TestWriteNotRecognisedBase64DataURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-file")
	_, err := WriteDataURLToFile("data:None;model/gltf-binary,SGVsbG8gVGhlcmU=", path)
	require.NoError(t, err)
}
