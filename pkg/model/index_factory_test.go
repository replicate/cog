package model

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

func TestIndexBuilder_BuildFromDescriptors(t *testing.T) {
	t.Run("builds index from image and weight descriptors", func(t *testing.T) {
		imgDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      1234,
			Digest: v1.Hash{
				Algorithm: "sha256",
				Hex:       "aaaa",
			},
		}
		weightDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      5678,
			Digest: v1.Hash{
				Algorithm: "sha256",
				Hex:       "bbbb",
			},
		}

		builder := NewIndexBuilder()
		builder.SetImageDescriptor(imgDesc, &v1.Platform{OS: "linux", Architecture: "amd64"})
		builder.AddWeightDescriptor(weightDesc, imgDesc.Digest.String(), "model-v1", "/cache/model.safetensors")

		idx, err := builder.BuildFromDescriptors()
		require.NoError(t, err)

		idxManifest, err := idx.IndexManifest()
		require.NoError(t, err)
		require.Len(t, idxManifest.Manifests, 2)

		// First entry: image with platform
		require.Equal(t, imgDesc.Digest, idxManifest.Manifests[0].Digest)
		require.Equal(t, imgDesc.Size, idxManifest.Manifests[0].Size)
		require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
		require.Equal(t, "amd64", idxManifest.Manifests[0].Platform.Architecture)

		// Second entry: weight artifact with unknown platform and annotations
		require.Equal(t, weightDesc.Digest, idxManifest.Manifests[1].Digest)
		require.Equal(t, weightDesc.Size, idxManifest.Manifests[1].Size)
		require.Equal(t, PlatformUnknown, idxManifest.Manifests[1].Platform.OS)
		require.Equal(t, PlatformUnknown, idxManifest.Manifests[1].Platform.Architecture)
		require.Equal(t, AnnotationValueWeights, idxManifest.Manifests[1].Annotations[AnnotationReferenceType])
		require.Equal(t, imgDesc.Digest.String(), idxManifest.Manifests[1].Annotations[AnnotationReferenceDigest])
		require.Equal(t, "model-v1", idxManifest.Manifests[1].Annotations[AnnotationWeightName])
		require.Equal(t, "/cache/model.safetensors", idxManifest.Manifests[1].Annotations[AnnotationWeightDest])
	})

	t.Run("builds index with multiple weight descriptors", func(t *testing.T) {
		imgDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      1000,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "img111"},
		}
		weight1 := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      2000,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "w1111"},
		}
		weight2 := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      3000,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "w2222"},
		}

		builder := NewIndexBuilder()
		builder.SetImageDescriptor(imgDesc, &v1.Platform{OS: "linux", Architecture: "amd64"})
		builder.AddWeightDescriptor(weight1, imgDesc.Digest.String(), "weight-1", "/weights/w1.bin")
		builder.AddWeightDescriptor(weight2, imgDesc.Digest.String(), "weight-2", "/weights/w2.bin")

		idx, err := builder.BuildFromDescriptors()
		require.NoError(t, err)

		idxManifest, err := idx.IndexManifest()
		require.NoError(t, err)
		require.Len(t, idxManifest.Manifests, 3) // 1 image + 2 weights
	})

	t.Run("requires image descriptor", func(t *testing.T) {
		builder := NewIndexBuilder()
		_, err := builder.BuildFromDescriptors()
		require.Error(t, err)
		require.Contains(t, err.Error(), "image descriptor not set")
	})

	t.Run("builds index without weight descriptors", func(t *testing.T) {
		imgDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      1234,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "aaaa"},
		}

		builder := NewIndexBuilder()
		builder.SetImageDescriptor(imgDesc, &v1.Platform{OS: "linux", Architecture: "amd64"})

		idx, err := builder.BuildFromDescriptors()
		require.NoError(t, err)

		idxManifest, err := idx.IndexManifest()
		require.NoError(t, err)
		require.Len(t, idxManifest.Manifests, 1)
	})

	t.Run("index has OCI media type", func(t *testing.T) {
		imgDesc := v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Size:      1234,
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "aaaa"},
		}

		builder := NewIndexBuilder()
		builder.SetImageDescriptor(imgDesc, &v1.Platform{OS: "linux", Architecture: "amd64"})

		idx, err := builder.BuildFromDescriptors()
		require.NoError(t, err)

		mt, err := idx.MediaType()
		require.NoError(t, err)
		require.Equal(t, types.OCIImageIndex, mt)
	})
}
