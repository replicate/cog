// Package ociartifact provides utilities for building OCI artifacts for model weights.
package ociartifact

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/replicate/cog/pkg/model"
)

// WeightsArtifactBuilder builds OCI artifacts containing model weights.
type WeightsArtifactBuilder struct {
	addendums []mutate.Addendum
}

// NewWeightsArtifactBuilder creates a new builder for weights artifacts.
func NewWeightsArtifactBuilder() *WeightsArtifactBuilder {
	return &WeightsArtifactBuilder{}
}

// AddLayer adds a weight file as a layer to the artifact.
func (b *WeightsArtifactBuilder) AddLayer(wf model.WeightFile, data []byte) error {
	// Create annotations for the layer
	annotations := map[string]string{
		model.AnnotationWeightsName:             wf.Name,
		model.AnnotationWeightsDest:             wf.Dest,
		model.AnnotationWeightsDigestOriginal:   wf.DigestOriginal,
		model.AnnotationWeightsSizeUncompressed: strconv.FormatInt(wf.SizeUncompressed, 10),
	}
	if wf.Source != "" {
		annotations[model.AnnotationWeightsSource] = wf.Source
	}

	// Create a static layer from the compressed data
	layer := static.NewLayer(data, types.MediaType(wf.MediaType))

	// Create an addendum with the layer and annotations
	addendum := mutate.Addendum{
		Layer:       layer,
		Annotations: annotations,
		MediaType:   types.MediaType(wf.MediaType),
	}

	b.addendums = append(b.addendums, addendum)
	return nil
}

// Build creates the OCI artifact image with all added layers.
func (b *WeightsArtifactBuilder) Build() (v1.Image, error) {
	// Start with an empty image and convert to OCI format
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)

	// Add all layers with their annotations
	if len(b.addendums) > 0 {
		var err error
		img, err = mutate.Append(img, b.addendums...)
		if err != nil {
			return nil, fmt.Errorf("appending layers: %w", err)
		}
	}

	// Wrap the image to set artifact type
	return &weightsArtifact{Image: img}, nil
}

// weightsArtifact wraps an image to implement artifact type.
type weightsArtifact struct {
	v1.Image
}

// ArtifactType implements the withArtifactType interface used by partial.ArtifactType.
func (a *weightsArtifact) ArtifactType() (string, error) {
	return model.MediaTypeWeightsManifest, nil
}

// AddLayersFromLock adds layers for all files in a WeightsLock.
// The baseDir is used to resolve file:// source URLs.
func (b *WeightsArtifactBuilder) AddLayersFromLock(lock *model.WeightsLock, baseDir string) error {
	for i := range lock.Files {
		wf := &lock.Files[i]

		// Resolve source path
		sourcePath, err := resolveSource(wf.Source, baseDir)
		if err != nil {
			return fmt.Errorf("resolve source for %s: %w", wf.Name, err)
		}

		// Read and compress file
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", sourcePath, err)
		}

		// Compute original digest
		originalHash := sha256.Sum256(data)
		wf.DigestOriginal = "sha256:" + hex.EncodeToString(originalHash[:])
		wf.SizeUncompressed = int64(len(data))

		// Compress
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		if _, err := io.Copy(gw, bytes.NewReader(data)); err != nil {
			return fmt.Errorf("compress %s: %w", wf.Name, err)
		}
		if err := gw.Close(); err != nil {
			return fmt.Errorf("close gzip for %s: %w", wf.Name, err)
		}

		// Compute compressed digest
		compressedHash := sha256.Sum256(compressed.Bytes())
		wf.Digest = "sha256:" + hex.EncodeToString(compressedHash[:])
		wf.Size = int64(compressed.Len())
		wf.MediaType = model.MediaTypeWeightsLayerGzip

		if err := b.AddLayer(*wf, compressed.Bytes()); err != nil {
			return fmt.Errorf("add layer %s: %w", wf.Name, err)
		}
	}
	return nil
}

func resolveSource(source, baseDir string) (string, error) {
	if strings.HasPrefix(source, "file://") {
		path := strings.TrimPrefix(source, "file://")
		// Handle relative paths (./something or just something)
		if !filepath.IsAbs(path) {
			// Remove leading ./ if present
			path = strings.TrimPrefix(path, "./")
			path = filepath.Join(baseDir, path)
		}
		return path, nil
	}
	// For now, only file:// sources are supported
	return "", fmt.Errorf("unsupported source scheme: %s (only file:// supported in placeholder)", source)
}
