package dind

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func NewContextFromDir(t *testing.T, dir string) io.Reader {
	return NewContextFromFS(t, os.DirFS(dir))
}

func NewContextFromFS(t *testing.T, fs fs.FS) io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := tw.AddFS(fs)
	require.NoError(t, err)
	tw.Close()

	return &buf
}
