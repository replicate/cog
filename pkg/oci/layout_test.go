package oci

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportOCILayout(t *testing.T) {
	t.Run("exports Docker tar to OCI layout", func(t *testing.T) {
		img, err := random.Image(512, 1)
		require.NoError(t, err)

		tag := "example.com/test/repo:v1"
		imageSave := fakeImageSave(img, tag)

		dir, exportedImg, err := ExportOCILayout(context.Background(), tag, imageSave)
		require.NoError(t, err)
		require.NotEmpty(t, dir)
		defer os.RemoveAll(dir)

		require.NotNil(t, exportedImg)

		// Verify the OCI layout directory was created with expected structure
		_, err = os.Stat(dir + "/oci-layout")
		require.NoError(t, err, "oci-layout file should exist")
		_, err = os.Stat(dir + "/index.json")
		require.NoError(t, err, "index.json should exist")

		// Verify the exported image can be re-loaded
		loaded, err := LoadOCILayoutImage(dir)
		require.NoError(t, err)
		require.NotNil(t, loaded)
	})

	t.Run("returns error for invalid image reference", func(t *testing.T) {
		imageSave := func(_ context.Context, _ string) (io.ReadCloser, error) {
			return nil, nil
		}
		_, _, err := ExportOCILayout(context.Background(), ":::invalid", imageSave)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse image reference")
	})

	t.Run("returns error when ImageSave fails", func(t *testing.T) {
		imageSave := func(_ context.Context, _ string) (io.ReadCloser, error) {
			return nil, errors.New("daemon error")
		}
		_, _, err := ExportOCILayout(context.Background(), "example.com/test:v1", imageSave)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "daemon error")
	})

	t.Run("returns error for invalid tar data", func(t *testing.T) {
		imageSave := func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader([]byte("not a valid tar"))), nil
		}
		_, _, err := ExportOCILayout(context.Background(), "example.com/test:v1", imageSave)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load image from tar")
	})
}

func TestLoadOCILayoutImage(t *testing.T) {
	t.Run("loads image from valid layout", func(t *testing.T) {
		img, err := random.Image(512, 2)
		require.NoError(t, err)

		dir := writeTestOCILayout(t, img)

		loaded, err := LoadOCILayoutImage(dir)
		require.NoError(t, err)
		require.NotNil(t, loaded)

		// Verify the loaded image has the same digest
		origDigest, err := img.Digest()
		require.NoError(t, err)
		loadedDigest, err := loaded.Digest()
		require.NoError(t, err)
		assert.Equal(t, origDigest, loadedDigest)
	})

	t.Run("returns error for nonexistent path", func(t *testing.T) {
		_, err := LoadOCILayoutImage("/nonexistent/path")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "open OCI layout")
	})

	t.Run("returns error for empty layout", func(t *testing.T) {
		dir := t.TempDir()
		_, err := layout.Write(dir, empty.Index)
		require.NoError(t, err)

		_, err = LoadOCILayoutImage(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OCI layout contains no images")
	})
}

// fakeImageSave creates a fake ImageSaveFunc that produces a Docker-format tar.
func fakeImageSave(img v1.Image, tagStr string) ImageSaveFunc {
	return func(_ context.Context, _ string) (io.ReadCloser, error) {
		tag, err := name.NewTag(tagStr, name.Insecure)
		if err != nil {
			return nil, fmt.Errorf("parse tag: %w", err)
		}
		var buf bytes.Buffer
		refToImage := map[name.Tag]v1.Image{tag: img}
		if err := tarball.MultiWrite(refToImage, &buf); err != nil {
			return nil, fmt.Errorf("create test tar: %w", err)
		}
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	}
}

// writeTestOCILayout creates a temporary OCI layout directory with the given image.
func writeTestOCILayout(t *testing.T, img v1.Image) string {
	t.Helper()
	dir := t.TempDir()
	idx := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	_, err := layout.Write(dir, idx)
	require.NoError(t, err)
	return dir
}
