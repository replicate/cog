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
		modelPath := filepath.Join(weightsDir, "model.bin")
		require.NoError(t, os.WriteFile(modelPath, testData, 0o644))

		lock := &WeightsLock{
			Version: "1",
			Created: time.Now().UTC(),
			Files: []WeightFile{
				{
					Name: "my-model-v1",
					Dest: "/cache/model.bin",
				},
			},
		}
		lockPath := filepath.Join(dir, "weights.lock")
		require.NoError(t, lock.Save(lockPath))

		filePaths := map[string]string{
			"my-model-v1": modelPath,
		}

		factory := NewIndexFactory()
		artifact, manifest, err := factory.BuildWeightsArtifact(context.Background(), lockPath, filePaths)
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
		modelPath := filepath.Join(weightsDir, "model.bin")
		require.NoError(t, os.WriteFile(modelPath, []byte("test"), 0o644))

		lock := &WeightsLock{
			Version: "1",
			Created: time.Now().UTC(),
			Files: []WeightFile{
				{
					Name: "my-model-v1",
					Dest: "/cache/model.bin",
				},
			},
		}
		lockPath := filepath.Join(dir, "weights.lock")
		require.NoError(t, lock.Save(lockPath))

		filePaths := map[string]string{
			"my-model-v1": modelPath,
		}

		factory := NewIndexFactory()

		weightsArtifact, _, err := factory.BuildWeightsArtifact(context.Background(), lockPath, filePaths)
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
		filePaths := map[string]string{"test": "/tmp/test"}
		_, _, err := factory.BuildWeightsArtifact(context.Background(), "/nonexistent/weights.lock", filePaths)
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
		modelPath := filepath.Join(weightsDir, "model.bin")
		require.NoError(t, os.WriteFile(modelPath, testData, 0o644))

		// Create a WeightsManifest (instead of WeightsLock)
		manifest := &WeightsManifest{
			ArtifactType: MediaTypeWeightsManifest,
			Created:      time.Now().UTC(),
			Files: []WeightFile{
				{
					Name: "my-model-v1",
					Dest: "/cache/model.bin",
				},
			},
		}

		filePaths := map[string]string{
			"my-model-v1": modelPath,
		}

		factory := NewIndexFactory()
		artifact, err := factory.BuildWeightsArtifactFromManifest(context.Background(), manifest, filePaths)
		require.NoError(t, err)
		require.NotNil(t, artifact)

		// Verify artifact has the expected layer
		layers, err := artifact.Layers()
		require.NoError(t, err)
		require.Len(t, layers, 1)
	})

	t.Run("returns error when file path not provided", func(t *testing.T) {
		manifest := &WeightsManifest{
			Files: []WeightFile{
				{
					Name: "missing-weight",
					Dest: "/cache/missing.bin",
				},
			},
		}

		// Empty filePaths map - weight name not found
		filePaths := map[string]string{}

		factory := NewIndexFactory()
		_, err := factory.BuildWeightsArtifactFromManifest(context.Background(), manifest, filePaths)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no file path provided")
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
			Name:             "llama-3.1-8b-weights",
			Dest:             "/cache/model.safetensors",
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
		require.Equal(t, "llama-3.1-8b-weights", layer.Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/model.safetensors", layer.Annotations[AnnotationWeightsDest])
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

	t.Run("layer has expected annotations", func(t *testing.T) {
		data := []byte("test data")
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		_, _ = gw.Write(data)
		_ = gw.Close()

		hash := sha256.Sum256(compressed.Bytes())
		digest := "sha256:" + hex.EncodeToString(hash[:])

		wf := WeightFile{
			Name:             "test-weight-v1",
			Dest:             "/cache/test.bin",
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
		require.Equal(t, "test-weight-v1", layer.Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/test.bin", layer.Annotations[AnnotationWeightsDest])
		require.Equal(t, "sha256:orig", layer.Annotations[AnnotationWeightsDigestOriginal])
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
					Name: "my-model-v1",
					Dest: "/cache/model.bin",
				},
			},
		}

		filePaths := map[string]string{
			"my-model-v1": testFile,
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayersFromLock(context.Background(), lock, filePaths)
		require.NoError(t, err)

		artifact, err := builder.Build()
		require.NoError(t, err)

		manifest, err := artifact.Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 1)

		layer := manifest.Layers[0]
		require.Equal(t, "my-model-v1", layer.Annotations[AnnotationWeightsName])
		require.Equal(t, "/cache/model.bin", layer.Annotations[AnnotationWeightsDest])
		require.NotEmpty(t, layer.Annotations[AnnotationWeightsDigestOriginal])
	})

	t.Run("resolve file path from map", func(t *testing.T) {
		dir := t.TempDir()
		testFile := filepath.Join(dir, "weights", "model.bin")
		require.NoError(t, os.MkdirAll(filepath.Dir(testFile), 0o755))
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0o644))

		lock := &WeightsLock{
			Version: "1",
			Files: []WeightFile{
				{
					Name: "my-model-v1",
					Dest: "/cache/model.bin",
				},
			},
		}

		filePaths := map[string]string{
			"my-model-v1": testFile,
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayersFromLock(context.Background(), lock, filePaths)
		require.NoError(t, err)
	})

	t.Run("missing file path in map", func(t *testing.T) {
		lock := &WeightsLock{
			Version: "1",
			Files: []WeightFile{
				{
					Name: "unknown-weight",
					Dest: "/cache/model.bin",
				},
			},
		}

		// filePaths doesn't contain "unknown-weight"
		filePaths := map[string]string{
			"other-weight": "/tmp/other.bin",
		}

		builder := NewWeightsArtifactBuilder()
		err := builder.AddLayersFromLock(context.Background(), lock, filePaths)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no file path provided")
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
