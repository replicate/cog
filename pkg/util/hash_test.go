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

func TestHashFileWithSaltAndRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.tmp")
	d1 := []byte("hello\nreplicate\nhello\n")
	err := os.WriteFile(path, d1, 0o644)
	require.NoError(t, err)

	_, err = SHA256HashFileWithSaltAndRange(path, 0, 60, "go\n")
	require.Error(t, err)

	_, err = SHA256HashFileWithSaltAndRange(path, 23, 1, "go\n")
	require.Error(t, err)

	sha256, err := SHA256HashFileWithSaltAndRange(path, 0, 6, "go\n")
	require.NoError(t, err)
	require.Equal(t, "43d250d92b5dbb47f75208de8e9a9a321d23e85eed0dc3d5dfa83bc3cc5aa68c", sha256)

	sha256, err = SHA256HashFileWithSaltAndRange(path, 16, 22, "go\n")
	require.NoError(t, err)
	require.Equal(t, "43d250d92b5dbb47f75208de8e9a9a321d23e85eed0dc3d5dfa83bc3cc5aa68c", sha256)
}
