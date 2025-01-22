package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.tmp")
	d1 := []byte("hello\ngo\n")
	err := os.WriteFile(path, d1, 0o644)
	require.NoError(t, err)

	sha256, err := SHA256HashFile(path)
	require.NoError(t, err)
	require.Equal(t, "43d250d92b5dbb47f75208de8e9a9a321d23e85eed0dc3d5dfa83bc3cc5aa68c", sha256)
}
