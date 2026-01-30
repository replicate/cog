// pkg/ociartifact/artifact_test.go
package ociartifact

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model"
)

func TestWeightsArtifactBuilder(t *testing.T) {
	t.Run("build empty artifact", func(t *testing.T) {
		builder := NewWeightsArtifactBuilder()
		artifact, err := builder.Build()
		require.NoError(t, err)

		mt, err := artifact.MediaType()
		require.NoError(t, err)
		require.Equal(t, types.OCIManifestSchema1, mt)

		artifactType, err := partial.ArtifactType(artifact)
		require.NoError(t, err)
		require.Equal(t, model.MediaTypeWeightsManifest, artifactType)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Empty(t, manifest.Layers)
	})

	t.Run("add layer from weight file", func(t *testing.T) {
		// Create test data and compress it
		data := []byte("test weight content")
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		gw.Write(data)
		gw.Close()

		hash := sha256.Sum256(compressed.Bytes())
		digest := "sha256:" + hex.EncodeToString(hash[:])

		wf := model.WeightFile{
			Name:             "test.bin",
			Dest:             "/cache/test.bin",
			Digest:           digest,
			DigestOriginal:   "sha256:original123",
			Size:             int64(compressed.Len()),
			SizeUncompressed: int64(len(data)),
			MediaType:        model.MediaTypeWeightsLayerGzip,
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayer(wf, compressed.Bytes())
		require.NoError(t, err)

		artifact, err := builder.Build()
		require.NoError(t, err)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 1)

		layer := manifest.Layers[0]
		require.Equal(t, types.MediaType(model.MediaTypeWeightsLayerGzip), layer.MediaType)
		require.Equal(t, "test.bin", layer.Annotations[model.AnnotationWeightsName])
		require.Equal(t, "/cache/test.bin", layer.Annotations[model.AnnotationWeightsDest])
	})

	t.Run("add layer with all annotations", func(t *testing.T) {
		data := []byte("test weight data")
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		gw.Write(data)
		gw.Close()

		hash := sha256.Sum256(compressed.Bytes())
		digest := "sha256:" + hex.EncodeToString(hash[:])

		wf := model.WeightFile{
			Name:             "model.safetensors",
			Dest:             "/cache/model.safetensors",
			Source:           "hf://meta-llama/Llama-3.1-8B",
			Digest:           digest,
			DigestOriginal:   "sha256:abc123def456",
			Size:             int64(compressed.Len()),
			SizeUncompressed: int64(len(data)),
			MediaType:        model.MediaTypeWeightsLayerGzip,
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayer(wf, compressed.Bytes())
		require.NoError(t, err)

		artifact, err := builder.Build()
		require.NoError(t, err)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 1)

		layer := manifest.Layers[0]
		require.Equal(t, "model.safetensors", layer.Annotations[model.AnnotationWeightsName])
		require.Equal(t, "/cache/model.safetensors", layer.Annotations[model.AnnotationWeightsDest])
		require.Equal(t, "hf://meta-llama/Llama-3.1-8B", layer.Annotations[model.AnnotationWeightsSource])
		require.Equal(t, "sha256:abc123def456", layer.Annotations[model.AnnotationWeightsDigestOriginal])
		require.Equal(t, "16", layer.Annotations[model.AnnotationWeightsSizeUncompressed])
	})

	t.Run("add multiple layers", func(t *testing.T) {
		builder := NewWeightsArtifactBuilder()

		// Add first layer
		data1 := []byte("first weight file")
		var compressed1 bytes.Buffer
		gw1 := gzip.NewWriter(&compressed1)
		gw1.Write(data1)
		gw1.Close()

		hash1 := sha256.Sum256(compressed1.Bytes())
		digest1 := "sha256:" + hex.EncodeToString(hash1[:])

		wf1 := model.WeightFile{
			Name:             "layer1.bin",
			Dest:             "/cache/layer1.bin",
			Digest:           digest1,
			DigestOriginal:   "sha256:first",
			Size:             int64(compressed1.Len()),
			SizeUncompressed: int64(len(data1)),
			MediaType:        model.MediaTypeWeightsLayerGzip,
		}
		err := builder.AddLayer(wf1, compressed1.Bytes())
		require.NoError(t, err)

		// Add second layer
		data2 := []byte("second weight file")
		var compressed2 bytes.Buffer
		gw2 := gzip.NewWriter(&compressed2)
		gw2.Write(data2)
		gw2.Close()

		hash2 := sha256.Sum256(compressed2.Bytes())
		digest2 := "sha256:" + hex.EncodeToString(hash2[:])

		wf2 := model.WeightFile{
			Name:             "layer2.bin",
			Dest:             "/cache/layer2.bin",
			Digest:           digest2,
			DigestOriginal:   "sha256:second",
			Size:             int64(compressed2.Len()),
			SizeUncompressed: int64(len(data2)),
			MediaType:        model.MediaTypeWeightsLayerGzip,
		}
		err = builder.AddLayer(wf2, compressed2.Bytes())
		require.NoError(t, err)

		artifact, err := builder.Build()
		require.NoError(t, err)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 2)

		// Verify first layer
		require.Equal(t, "layer1.bin", manifest.Layers[0].Annotations[model.AnnotationWeightsName])
		require.Equal(t, "/cache/layer1.bin", manifest.Layers[0].Annotations[model.AnnotationWeightsDest])

		// Verify second layer
		require.Equal(t, "layer2.bin", manifest.Layers[1].Annotations[model.AnnotationWeightsName])
		require.Equal(t, "/cache/layer2.bin", manifest.Layers[1].Annotations[model.AnnotationWeightsDest])
	})

	t.Run("layer without source annotation", func(t *testing.T) {
		data := []byte("test data")
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		gw.Write(data)
		gw.Close()

		hash := sha256.Sum256(compressed.Bytes())
		digest := "sha256:" + hex.EncodeToString(hash[:])

		wf := model.WeightFile{
			Name:             "test.bin",
			Dest:             "/cache/test.bin",
			Source:           "", // No source
			Digest:           digest,
			DigestOriginal:   "sha256:orig",
			Size:             int64(compressed.Len()),
			SizeUncompressed: int64(len(data)),
			MediaType:        model.MediaTypeWeightsLayerGzip,
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayer(wf, compressed.Bytes())
		require.NoError(t, err)

		artifact, err := builder.Build()
		require.NoError(t, err)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 1)

		layer := manifest.Layers[0]
		// Source annotation should not be present when empty
		_, hasSource := layer.Annotations[model.AnnotationWeightsSource]
		require.False(t, hasSource, "source annotation should not be present when empty")
	})
}
