// pkg/model/index_factory_test.go
package model

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

func TestIndexFactory(t *testing.T) {
	t.Run("build weights artifact", func(t *testing.T) {
		dir := t.TempDir()
		weightsDir := filepath.Join(dir, "weights")
		require.NoError(t, os.MkdirAll(weightsDir, 0o755))

		testData := []byte("fake model weights for testing purposes only")
		require.NoError(t, os.WriteFile(filepath.Join(weightsDir, "model.bin"), testData, 0o644))

		lock := &WeightsLock{
			Version: "1",
			Created: time.Now().UTC(),
			Files: []WeightFile{
				{
					Name:   "model.bin",
					Dest:   "/cache/model.bin",
					Source: "file://./weights/model.bin",
				},
			},
		}
		lockPath := filepath.Join(dir, "weights.lock")
		require.NoError(t, lock.Save(lockPath))

		factory := NewIndexFactory()
		artifact, manifest, err := factory.BuildWeightsArtifact(context.Background(), lockPath, dir)
		require.NoError(t, err)
		require.NotNil(t, artifact)
		require.NotNil(t, manifest)
		require.Len(t, manifest.Files, 1)
		require.NotEmpty(t, manifest.Files[0].Digest)
		require.NotEmpty(t, manifest.Files[0].DigestOriginal)
		require.Equal(t, MediaTypeWeightsLayerGzip, manifest.Files[0].MediaType)

		artifactType, err := partial.ArtifactType(artifact)
		require.NoError(t, err)
		require.Equal(t, MediaTypeWeightsManifest, artifactType)
	})

	t.Run("build index with image and weights", func(t *testing.T) {
		testImg := empty.Image
		testImg, _ = mutate.Config(testImg, v1.Config{})

		dir := t.TempDir()
		weightsDir := filepath.Join(dir, "weights")
		require.NoError(t, os.MkdirAll(weightsDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(weightsDir, "model.bin"), []byte("test"), 0o644))

		lock := &WeightsLock{
			Version: "1",
			Created: time.Now().UTC(),
			Files: []WeightFile{
				{
					Name:   "model.bin",
					Dest:   "/cache/model.bin",
					Source: "file://./weights/model.bin",
				},
			},
		}
		lockPath := filepath.Join(dir, "weights.lock")
		require.NoError(t, lock.Save(lockPath))

		factory := NewIndexFactory()

		weightsArtifact, _, err := factory.BuildWeightsArtifact(context.Background(), lockPath, dir)
		require.NoError(t, err)

		platform := &Platform{OS: "linux", Architecture: "amd64"}
		idx, err := factory.BuildIndex(context.Background(), testImg, weightsArtifact, platform)
		require.NoError(t, err)

		idxManifest, err := idx.IndexManifest()
		require.NoError(t, err)
		require.Len(t, idxManifest.Manifests, 2)

		// First should be image, second should be weights
		require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
		require.Equal(t, "unknown", idxManifest.Manifests[1].Platform.OS)
		require.Equal(t, "weights", idxManifest.Manifests[1].Annotations[AnnotationReferenceType])
	})

	t.Run("weights lock not found", func(t *testing.T) {
		factory := NewIndexFactory()
		_, _, err := factory.BuildWeightsArtifact(context.Background(), "/nonexistent/weights.lock", "/tmp")
		require.Error(t, err)
		require.Contains(t, err.Error(), "load weights lock")
	})
}

func TestIndexFactory_BuildWeightsArtifactFromManifest(t *testing.T) {
	t.Run("builds artifact from weights manifest", func(t *testing.T) {
		dir := t.TempDir()
		weightsDir := filepath.Join(dir, "weights")
		require.NoError(t, os.MkdirAll(weightsDir, 0o755))

		testData := []byte("test model weights content")
		require.NoError(t, os.WriteFile(filepath.Join(weightsDir, "model.bin"), testData, 0o644))

		// Create a WeightsManifest (instead of WeightsLock)
		manifest := &WeightsManifest{
			ArtifactType: MediaTypeWeightsManifest,
			Created:      time.Now().UTC(),
			Files: []WeightFile{
				{
					Name:   "model.bin",
					Dest:   "/cache/model.bin",
					Source: "file://./weights/model.bin",
				},
			},
		}

		factory := NewIndexFactory()
		artifact, err := factory.BuildWeightsArtifactFromManifest(context.Background(), manifest, dir)
		require.NoError(t, err)
		require.NotNil(t, artifact)

		// Verify artifact has the expected layer
		layers, err := artifact.Layers()
		require.NoError(t, err)
		require.Len(t, layers, 1)
	})

	t.Run("returns error when source file not found", func(t *testing.T) {
		manifest := &WeightsManifest{
			Files: []WeightFile{
				{
					Name:   "missing.bin",
					Dest:   "/cache/missing.bin",
					Source: "file://./missing/weights.bin",
				},
			},
		}

		factory := NewIndexFactory()
		_, err := factory.BuildWeightsArtifactFromManifest(context.Background(), manifest, "/nonexistent/dir")
		require.Error(t, err)
	})
}

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
		require.Equal(t, MediaTypeWeightsManifest, artifactType)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Empty(t, manifest.Layers)
	})

	t.Run("add layer from weight file", func(t *testing.T) {
		data := []byte("test weight content")
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		_, _ = gw.Write(data)
		_ = gw.Close()

		hash := sha256.Sum256(compressed.Bytes())
		digest := "sha256:" + hex.EncodeToString(hash[:])

		wf := WeightFile{
			Name:             "test.bin",
			Dest:             "/cache/test.bin",
			Digest:           digest,
			DigestOriginal:   "sha256:original123",
			Size:             int64(compressed.Len()),
			SizeUncompressed: int64(len(data)),
			MediaType:        MediaTypeWeightsLayerGzip,
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
		require.Equal(t, types.MediaType(MediaTypeWeightsLayerGzip), layer.MediaType)
		require.Equal(t, "test.bin", layer.Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/test.bin", layer.Annotations[AnnotationWeightsDest])
	})

	t.Run("add layer with all annotations", func(t *testing.T) {
		data := []byte("test weight data")
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		_, _ = gw.Write(data)
		_ = gw.Close()

		hash := sha256.Sum256(compressed.Bytes())
		digest := "sha256:" + hex.EncodeToString(hash[:])

		wf := WeightFile{
			Name:             "model.safetensors",
			Dest:             "/cache/model.safetensors",
			Source:           "hf://meta-llama/Llama-3.1-8B",
			Digest:           digest,
			DigestOriginal:   "sha256:abc123def456",
			Size:             int64(compressed.Len()),
			SizeUncompressed: int64(len(data)),
			MediaType:        MediaTypeWeightsLayerGzip,
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
		require.Equal(t, "model.safetensors", layer.Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/model.safetensors", layer.Annotations[AnnotationWeightsDest])
		require.Equal(t, "hf://meta-llama/Llama-3.1-8B", layer.Annotations[AnnotationWeightsSource])
		require.Equal(t, "sha256:abc123def456", layer.Annotations[AnnotationWeightsDigestOriginal])
		require.Equal(t, "16", layer.Annotations[AnnotationWeightsSizeUncompressed])
	})

	t.Run("add multiple layers", func(t *testing.T) {
		builder := NewWeightsArtifactBuilder()

		// Add first layer
		data1 := []byte("first weight file")
		var compressed1 bytes.Buffer
		gw1 := gzip.NewWriter(&compressed1)
		_, _ = gw1.Write(data1)
		_ = gw1.Close()

		hash1 := sha256.Sum256(compressed1.Bytes())
		digest1 := "sha256:" + hex.EncodeToString(hash1[:])

		wf1 := WeightFile{
			Name:             "layer1.bin",
			Dest:             "/cache/layer1.bin",
			Digest:           digest1,
			DigestOriginal:   "sha256:first",
			Size:             int64(compressed1.Len()),
			SizeUncompressed: int64(len(data1)),
			MediaType:        MediaTypeWeightsLayerGzip,
		}
		err := builder.AddLayer(wf1, compressed1.Bytes())
		require.NoError(t, err)

		// Add second layer
		data2 := []byte("second weight file")
		var compressed2 bytes.Buffer
		gw2 := gzip.NewWriter(&compressed2)
		_, _ = gw2.Write(data2)
		_ = gw2.Close()

		hash2 := sha256.Sum256(compressed2.Bytes())
		digest2 := "sha256:" + hex.EncodeToString(hash2[:])

		wf2 := WeightFile{
			Name:             "layer2.bin",
			Dest:             "/cache/layer2.bin",
			Digest:           digest2,
			DigestOriginal:   "sha256:second",
			Size:             int64(compressed2.Len()),
			SizeUncompressed: int64(len(data2)),
			MediaType:        MediaTypeWeightsLayerGzip,
		}
		err = builder.AddLayer(wf2, compressed2.Bytes())
		require.NoError(t, err)

		artifact, err := builder.Build()
		require.NoError(t, err)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 2)

		require.Equal(t, "layer1.bin", manifest.Layers[0].Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/layer1.bin", manifest.Layers[0].Annotations[AnnotationWeightsDest])

		require.Equal(t, "layer2.bin", manifest.Layers[1].Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/layer2.bin", manifest.Layers[1].Annotations[AnnotationWeightsDest])
	})

	t.Run("layer without source annotation", func(t *testing.T) {
		data := []byte("test data")
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		_, _ = gw.Write(data)
		_ = gw.Close()

		hash := sha256.Sum256(compressed.Bytes())
		digest := "sha256:" + hex.EncodeToString(hash[:])

		wf := WeightFile{
			Name:             "test.bin",
			Dest:             "/cache/test.bin",
			Source:           "", // No source
			Digest:           digest,
			DigestOriginal:   "sha256:orig",
			Size:             int64(compressed.Len()),
			SizeUncompressed: int64(len(data)),
			MediaType:        MediaTypeWeightsLayerGzip,
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
		_, hasSource := layer.Annotations[AnnotationWeightsSource]
		require.False(t, hasSource, "source annotation should not be present when empty")
	})
}

func TestWeightsArtifactBuilderFromFiles(t *testing.T) {
	t.Run("build from weights lock and files", func(t *testing.T) {
		dir := t.TempDir()
		testData := []byte("test model weights content for testing")
		testFile := filepath.Join(dir, "model.bin")
		require.NoError(t, os.WriteFile(testFile, testData, 0o644))

		lock := &WeightsLock{
			Version: "1",
			Created: time.Now().UTC(),
			Files: []WeightFile{
				{
					Name:   "model.bin",
					Dest:   "/cache/model.bin",
					Source: "file://" + testFile,
				},
			},
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayersFromLock(lock, dir)
		require.NoError(t, err)

		artifact, err := builder.Build()
		require.NoError(t, err)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 1)

		layer := manifest.Layers[0]
		require.Equal(t, "model.bin", layer.Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/model.bin", layer.Annotations[AnnotationWeightsDest])
		require.NotEmpty(t, layer.Annotations[AnnotationWeightsDigestOriginal])
	})

	t.Run("resolve file source", func(t *testing.T) {
		dir := t.TempDir()
		testFile := filepath.Join(dir, "weights", "model.bin")
		require.NoError(t, os.MkdirAll(filepath.Dir(testFile), 0o755))
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o644))

		lock := &WeightsLock{
			Version: "1",
			Files: []WeightFile{
				{
					Name:   "model.bin",
					Dest:   "/cache/model.bin",
					Source: "file://./weights/model.bin",
				},
			},
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayersFromLock(lock, dir)
		require.NoError(t, err)
	})

	t.Run("unsupported source scheme", func(t *testing.T) {
		lock := &WeightsLock{
			Version: "1",
			Files: []WeightFile{
				{
					Name:   "model.bin",
					Dest:   "/cache/model.bin",
					Source: "hf://user/repo/model.bin",
				},
			},
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayersFromLock(lock, "/tmp")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported source scheme")
	})
}

func TestIndexBuilder(t *testing.T) {
	t.Run("build index with image and weights", func(t *testing.T) {
		testImg := empty.Image
		testImg, _ = mutate.Config(testImg, v1.Config{})

		weightsBuilder := NewWeightsArtifactBuilder()
		weightsArtifact, err := weightsBuilder.Build()
		require.NoError(t, err)

		imgDigest, _ := testImg.Digest()

		builder := NewIndexBuilder()
		builder.SetModelImage(testImg, &v1.Platform{OS: "linux", Architecture: "amd64"})
		builder.SetWeightsArtifact(weightsArtifact, imgDigest.String())

		idx, err := builder.Build()
		require.NoError(t, err)

		idxManifest, err := idx.IndexManifest()
		require.NoError(t, err)
		require.Len(t, idxManifest.Manifests, 2)

		require.Equal(t, "linux", idxManifest.Manifests[0].Platform.OS)
		require.Equal(t, "amd64", idxManifest.Manifests[0].Platform.Architecture)

		require.Equal(t, "unknown", idxManifest.Manifests[1].Platform.OS)
		require.Equal(t, "unknown", idxManifest.Manifests[1].Platform.Architecture)
		require.Equal(t, "weights", idxManifest.Manifests[1].Annotations[AnnotationReferenceType])
		require.Equal(t, imgDigest.String(), idxManifest.Manifests[1].Annotations[AnnotationReferenceDigest])
	})

	t.Run("build index without weights", func(t *testing.T) {
		testImg := empty.Image
		testImg, _ = mutate.Config(testImg, v1.Config{})

		builder := NewIndexBuilder()
		builder.SetModelImage(testImg, &v1.Platform{OS: "linux", Architecture: "amd64"})

		idx, err := builder.Build()
		require.NoError(t, err)

		idxManifest, err := idx.IndexManifest()
		require.NoError(t, err)
		require.Len(t, idxManifest.Manifests, 1)
	})

	t.Run("build index requires model image", func(t *testing.T) {
		builder := NewIndexBuilder()
		_, err := builder.Build()
		require.Error(t, err)
		require.Contains(t, err.Error(), "model image not set")
	})

	t.Run("verify OCI index media type", func(t *testing.T) {
		testImg := empty.Image
		testImg, _ = mutate.Config(testImg, v1.Config{})

		builder := NewIndexBuilder()
		builder.SetModelImage(testImg, &v1.Platform{OS: "linux", Architecture: "amd64"})

		idx, err := builder.Build()
		require.NoError(t, err)

		mt, err := idx.MediaType()
		require.NoError(t, err)
		require.Equal(t, types.OCIImageIndex, mt)
	})
}
