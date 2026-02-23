package registry

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/replicate/cog/pkg/util/console"
)

// ExportOCILayout exports an image from the Docker daemon to an OCI layout directory.
// It uses Docker's ImageSave API to get a Docker tar stream, converts it to a v1.Image
// using go-containerregistry, then writes it as an OCI layout.
//
// The caller is responsible for cleaning up the returned directory when done.
func ExportOCILayout(ctx context.Context, imageRef string, imageSave func(ctx context.Context, imageRef string) (io.ReadCloser, error)) (string, v1.Image, error) {
	console.Debugf("Exporting image %s from Docker daemon...", imageRef)

	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return "", nil, fmt.Errorf("parse image reference %q: %w", imageRef, err)
	}

	// Get the Docker tar stream
	rc, err := imageSave(ctx, imageRef)
	if err != nil {
		return "", nil, fmt.Errorf("export image from daemon: %w", err)
	}
	defer rc.Close() //nolint:errcheck

	// Write the tar to a temp file so we can seek on it
	tmpTar, err := os.CreateTemp("", "cog-image-*.tar")
	if err != nil {
		return "", nil, fmt.Errorf("create temp tar file: %w", err)
	}
	defer func() { _ = os.Remove(tmpTar.Name()) }()
	defer tmpTar.Close() //nolint:errcheck

	if _, err := io.Copy(tmpTar, rc); err != nil {
		return "", nil, fmt.Errorf("write image tar: %w", err)
	}
	_ = rc.Close()

	// Load image from Docker tar using go-containerregistry
	tag, ok := ref.(name.Tag)
	if !ok {
		// If reference is a digest, use tag "latest" as a fallback
		tag = ref.Context().Tag("latest")
	}

	img, err := tarball.ImageFromPath(tmpTar.Name(), &tag)
	if err != nil {
		return "", nil, fmt.Errorf("load image from tar: %w", err)
	}

	// Create a temp directory for the OCI layout
	dir, err := os.MkdirTemp("", "cog-oci-layout-*")
	if err != nil {
		return "", nil, fmt.Errorf("create OCI layout directory: %w", err)
	}

	console.Debugf("Writing OCI layout to %s", dir)
	lp, err := layout.Write(dir, empty.Index)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("initialize OCI layout: %w", err)
	}

	if err := lp.AppendImage(img); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("write image to OCI layout: %w", err)
	}

	return dir, img, nil
}

// LoadOCILayoutImage loads the first image from an OCI layout directory.
func LoadOCILayoutImage(layoutPath string) (v1.Image, error) {
	lp, err := layout.FromPath(layoutPath)
	if err != nil {
		return nil, fmt.Errorf("open OCI layout at %s: %w", layoutPath, err)
	}

	idx, err := lp.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("read OCI layout index: %w", err)
	}

	idxManifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read index manifest: %w", err)
	}

	if len(idxManifest.Manifests) == 0 {
		return nil, fmt.Errorf("OCI layout contains no images")
	}

	// Use the first manifest (we only export one image)
	desc := idxManifest.Manifests[0]
	img, err := idx.Image(desc.Digest)
	if err != nil {
		return nil, fmt.Errorf("load image from layout: %w", err)
	}

	return img, nil
}
