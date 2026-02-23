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

		// Verify the exported image has layers
		layers, err := exportedImg.Layers()
		require.NoError(t, err)
		assert.NotEmpty(t, layers)
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
