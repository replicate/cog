package model

import (
	"fmt"
	"strconv"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// IndexBuilder builds an OCI Image Index from pre-pushed manifest descriptors.
type IndexBuilder struct {
	imageDescriptor   *v1.Descriptor
	imagePlatform     *v1.Platform
	weightDescriptors []weightDescEntry
}

// weightDescEntry pairs a weight descriptor with its index-level metadata.
type weightDescEntry struct {
	descriptor       v1.Descriptor
	name             string
	setDigest        string
	uncompressedSize int64
}

// NewIndexBuilder creates a new index builder.
func NewIndexBuilder() *IndexBuilder {
	return &IndexBuilder{}
}

// SetImageDescriptor sets the image manifest descriptor.
func (b *IndexBuilder) SetImageDescriptor(desc v1.Descriptor, platform *v1.Platform) {
	b.imageDescriptor = &desc
	b.imagePlatform = platform
}

// AddWeightDescriptor adds a weight manifest descriptor.
// Metadata (name, setDigest, uncompressedSize) is hoisted into index-level
// annotations so the index is inspectable without fetching child manifests (§2.6).
func (b *IndexBuilder) AddWeightDescriptor(desc v1.Descriptor, name, setDigest string, uncompressedSize int64) {
	b.weightDescriptors = append(b.weightDescriptors, weightDescEntry{
		descriptor:       desc,
		name:             name,
		setDigest:        setDigest,
		uncompressedSize: uncompressedSize,
	})
}

// BuildFromDescriptors creates an OCI Image Index from pre-pushed manifest descriptors.
// This works with bare descriptors returned from push operations, avoiding the need
// to fetch images back from the registry.
func (b *IndexBuilder) BuildFromDescriptors() (v1.ImageIndex, error) {
	if b.imageDescriptor == nil {
		return nil, fmt.Errorf("image descriptor not set")
	}

	idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)

	// Add image manifest
	idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
		Add: &descriptorAppendable{desc: *b.imageDescriptor},
		Descriptor: v1.Descriptor{
			MediaType: b.imageDescriptor.MediaType,
			Size:      b.imageDescriptor.Size,
			Digest:    b.imageDescriptor.Digest,
			Platform:  b.imagePlatform,
		},
	})

	// Add weight manifest(s). Per spec §2.6, weight descriptors in the
	// index carry name and set-digest so the index is inspectable without
	// fetching child manifests.
	for _, entry := range b.weightDescriptors {
		annotations := make(map[string]string, 3)
		if entry.name != "" {
			annotations[AnnotationV1WeightName] = entry.name
		}
		if entry.setDigest != "" {
			annotations[AnnotationV1WeightSetDigest] = entry.setDigest
		}
		if entry.uncompressedSize > 0 {
			annotations[AnnotationV1WeightSizeUncomp] = strconv.FormatInt(entry.uncompressedSize, 10)
		}

		weightDesc := entry.descriptor
		weightDesc.ArtifactType = MediaTypeWeightArtifact

		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add: &descriptorAppendable{desc: weightDesc},
			Descriptor: v1.Descriptor{
				MediaType:    entry.descriptor.MediaType,
				Size:         entry.descriptor.Size,
				Digest:       entry.descriptor.Digest,
				ArtifactType: MediaTypeWeightArtifact,
				Platform: &v1.Platform{
					OS:           PlatformUnknown,
					Architecture: PlatformUnknown,
				},
				Annotations: annotations,
			},
		})
	}

	return idx, nil
}

// descriptorAppendable wraps a v1.Descriptor to implement mutate.Appendable.
// This allows building an OCI index from descriptors without needing full v1.Image objects.
type descriptorAppendable struct {
	desc v1.Descriptor
}

func (d *descriptorAppendable) MediaType() (types.MediaType, error) {
	return d.desc.MediaType, nil
}

func (d *descriptorAppendable) Digest() (v1.Hash, error) {
	return d.desc.Digest, nil
}

func (d *descriptorAppendable) Size() (int64, error) {
	return d.desc.Size, nil
}

func (d *descriptorAppendable) ArtifactType() (string, error) {
	return d.desc.ArtifactType, nil
}
