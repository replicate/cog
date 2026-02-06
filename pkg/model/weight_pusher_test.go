package model

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
)

func TestWeightPusher_Push_ReturnsErrorForNilArtifact(t *testing.T) {
	reg := &mockRegistry{}
	pusher := NewWeightPusher(reg)

	_, err := pusher.Push(context.Background(), "r8.im/user/model", nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "artifact is nil")
}

func TestWeightPusher_Push_ReturnsErrorForMissingFile(t *testing.T) {
	reg := &mockRegistry{}
	pusher := NewWeightPusher(reg)

	artifact := NewWeightArtifact("model-v1", v1.Descriptor{}, "/nonexistent/path/weights.bin", "/weights/model.bin", WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "model-v1",
		Target:        "/weights/model.bin",
		Created:       time.Now().UTC(),
	})

	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "weight file")
}

func TestWeightPusher_Push_PushesCorrectOCIArtifact(t *testing.T) {
	// Create a temp weight file
	dir := t.TempDir()
	weightPath := filepath.Join(dir, "model.safetensors")
	weightContent := []byte("fake weight data for testing tarball layer creation")
	require.NoError(t, os.WriteFile(weightPath, weightContent, 0o644))

	created := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	cfg := WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "model-v1",
		Target:        "/weights/model.safetensors",
		Created:       created,
	}

	artifact := NewWeightArtifact("model-v1", v1.Descriptor{}, weightPath, "/weights/model.safetensors", cfg)

	// Capture what gets pushed
	var pushedRef string
	var pushedImg v1.Image
	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			pushedRef = ref
			pushedImg = img
			return nil
		},
	}

	pusher := NewWeightPusher(reg)
	result, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the image was pushed to the correct repo
	require.Equal(t, "r8.im/user/model", pushedRef)
	require.NotNil(t, pushedImg)

	// Verify manifest structure
	manifest, err := pushedImg.Manifest()
	require.NoError(t, err)
	require.Equal(t, types.OCIManifestSchema1, manifest.MediaType)

	// Verify config blob has correct media type
	require.Equal(t, types.MediaType(MediaTypeWeightConfig), manifest.Config.MediaType)

	// Verify config blob content is correct WeightConfig JSON
	configBlob, err := pushedImg.RawConfigFile()
	require.NoError(t, err)
	var parsedConfig WeightConfig
	require.NoError(t, json.Unmarshal(configBlob, &parsedConfig))
	require.Equal(t, "1.0", parsedConfig.SchemaVersion)
	require.Equal(t, "0.15.0", parsedConfig.CogVersion)
	require.Equal(t, "model-v1", parsedConfig.Name)
	require.Equal(t, "/weights/model.safetensors", parsedConfig.Target)
	require.Equal(t, created, parsedConfig.Created)

	// Verify there's exactly one layer (single file = single layer)
	require.Len(t, manifest.Layers, 1)

	// Verify layer media type
	require.Equal(t, types.MediaType(MediaTypeWeightLayer), manifest.Layers[0].MediaType)

	// Verify layer size matches the tarball wrapping of the weight file
	// (tarball will be larger than raw content due to tar headers)
	require.Greater(t, manifest.Layers[0].Size, int64(0))

	// Verify the result contains a valid descriptor
	require.NotEmpty(t, result.Descriptor.Digest.String())
	require.Greater(t, result.Descriptor.Size, int64(0))
}

func TestWeightPusher_Push_PropagatesPushError(t *testing.T) {
	dir := t.TempDir()
	weightPath := filepath.Join(dir, "model.bin")
	require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

	artifact := NewWeightArtifact("model-v1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "model-v1",
		Target:        "/weights/model.bin",
		Created:       time.Now().UTC(),
	})

	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			return fmt.Errorf("unauthorized: authentication required")
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "push weight manifest")
	require.Contains(t, err.Error(), "unauthorized")
}

func TestWeightPusher_Push_RawManifestContainsArtifactType(t *testing.T) {
	dir := t.TempDir()
	weightPath := filepath.Join(dir, "model.bin")
	require.NoError(t, os.WriteFile(weightPath, []byte("test weight data"), 0o644))

	artifact := NewWeightArtifact("model-v1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "model-v1",
		Target:        "/weights/model.bin",
		Created:       time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC),
	})

	var pushedImg v1.Image
	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			pushedImg = img
			return nil
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(context.Background(), "r8.im/user/model", artifact)
	require.NoError(t, err)

	// Parse raw manifest JSON to verify artifactType field
	rawManifest, err := pushedImg.RawManifest()
	require.NoError(t, err)

	var manifestJSON map[string]interface{}
	require.NoError(t, json.Unmarshal(rawManifest, &manifestJSON))

	// artifactType must be present at the manifest level (OCI 1.1)
	require.Equal(t, MediaTypeWeightArtifact, manifestJSON["artifactType"])

	// config.mediaType must be the weight config type
	configMap, ok := manifestJSON["config"].(map[string]interface{})
	require.True(t, ok, "config should be an object")
	require.Equal(t, MediaTypeWeightConfig, configMap["mediaType"])

	// layers should have exactly one entry with the weight layer media type
	layers, ok := manifestJSON["layers"].([]interface{})
	require.True(t, ok, "layers should be an array")
	require.Len(t, layers, 1)

	layerMap, ok := layers[0].(map[string]interface{})
	require.True(t, ok, "layer should be an object")
	require.Equal(t, MediaTypeWeightLayer, layerMap["mediaType"])
}

func TestWeightPusher_Push_ReturnsErrorForEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	weightPath := filepath.Join(dir, "model.bin")
	require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

	artifact := NewWeightArtifact("model-v1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "model-v1",
		Target:        "/weights/model.bin",
		Created:       time.Now().UTC(),
	})

	reg := &mockRegistry{}
	pusher := NewWeightPusher(reg)

	_, err := pusher.Push(context.Background(), "", artifact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "repo is required")
}

func TestWeightPusher_Push_PropagatesContextCancellation(t *testing.T) {
	dir := t.TempDir()
	weightPath := filepath.Join(dir, "model.bin")
	require.NoError(t, os.WriteFile(weightPath, []byte("test"), 0o644))

	artifact := NewWeightArtifact("model-v1", v1.Descriptor{}, weightPath, "/weights/model.bin", WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    "0.15.0",
		Name:          "model-v1",
		Target:        "/weights/model.bin",
		Created:       time.Now().UTC(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	reg := &mockRegistry{
		pushImageFunc: func(ctx context.Context, ref string, img v1.Image) error {
			return ctx.Err()
		},
	}

	pusher := NewWeightPusher(reg)
	_, err := pusher.Push(ctx, "r8.im/user/model", artifact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "context canceled")
}
