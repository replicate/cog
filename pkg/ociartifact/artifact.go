// Package ociartifact provides utilities for building OCI artifacts for model weights.
package ociartifact

import (
	"fmt"
	"strconv"

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
