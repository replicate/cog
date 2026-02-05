package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestWeightBuilder_HappyPath(t *testing.T) {
	// Setup: real temp file as a weight source
	tmpDir := t.TempDir()
	weightContent := []byte("test weight data for builder")
	weightFile := filepath.Join(tmpDir, "model.safetensors")
	err := os.WriteFile(weightFile, weightContent, 0o644)
	require.NoError(t, err)

	// Compute expected digest
	hash := sha256.Sum256(weightContent)
	expectedDigest := "sha256:" + hex.EncodeToString(hash[:])

	// Create source with config that has one weight
	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "my-model", Source: "model.safetensors", Target: "/srv/weights/model.safetensors"},
		},
	}, tmpDir)

	// Create a WeightBuilder
	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	// Build from the weight spec
	spec := NewWeightSpec("my-model", "model.safetensors", "/srv/weights/model.safetensors")

	artifact, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)
	require.NotNil(t, artifact)

	// Type assertion: should be a *WeightArtifact
	wa, ok := artifact.(*WeightArtifact)
	require.True(t, ok, "expected *WeightArtifact, got %T", artifact)

	// Check artifact interface
	require.Equal(t, ArtifactTypeWeight, wa.Type())
	require.Equal(t, "my-model", wa.Name())

	// Check descriptor
	desc := wa.Descriptor()
	require.Equal(t, expectedDigest, desc.Digest.String())
	require.Equal(t, int64(len(weightContent)), desc.Size)

	// Check weight-specific fields
	require.Equal(t, weightFile, wa.FilePath)
	require.Equal(t, "/srv/weights/model.safetensors", wa.Target)

	// Check WeightConfig
	require.Equal(t, "1.0", wa.Config.SchemaVersion)
	require.Equal(t, "0.15.0", wa.Config.CogVersion)
	require.Equal(t, "my-model", wa.Config.Name)
	require.Equal(t, "/srv/weights/model.safetensors", wa.Config.Target)
	require.False(t, wa.Config.Created.IsZero(), "Created should be set")
}

func TestWeightBuilder_WritesLockfile(t *testing.T) {
	// After Build(), a weights.lock should be written/updated at lockPath.
	tmpDir := t.TempDir()
	weightContent := []byte("lockfile test weight")
	err := os.WriteFile(filepath.Join(tmpDir, "model.bin"), weightContent, 0o644)
	require.NoError(t, err)

	hash := sha256.Sum256(weightContent)
	expectedDigest := "sha256:" + hex.EncodeToString(hash[:])

	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "my-model", Source: "model.bin", Target: "/weights/model.bin"},
		},
	}, tmpDir)

	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	spec := NewWeightSpec("my-model", "model.bin", "/weights/model.bin")
	_, err = wb.Build(context.Background(), spec)
	require.NoError(t, err)

	// Lockfile should exist
	_, err = os.Stat(lockPath)
	require.NoError(t, err, "lockfile should be created")

	// Load and verify lockfile contents
	lock, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Equal(t, "1.0", lock.Version)
	require.Len(t, lock.Files, 1)

	wf := lock.Files[0]
	require.Equal(t, "my-model", wf.Name)
	require.Equal(t, "/weights/model.bin", wf.Dest)
	require.Equal(t, expectedDigest, wf.Digest)
	require.Equal(t, int64(len(weightContent)), wf.Size)
}

func TestWeightBuilder_UpdatesExistingLockfile(t *testing.T) {
	// If a lockfile already exists with entries, Build() should add/update the entry
	// for the built weight without losing other entries.
	tmpDir := t.TempDir()

	// Create two weight files
	content1 := []byte("weight one data")
	content2 := []byte("weight two data")
	err := os.WriteFile(filepath.Join(tmpDir, "w1.bin"), content1, 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpDir, "w2.bin"), content2, 0o644)
	require.NoError(t, err)

	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "weight-1", Source: "w1.bin", Target: "/weights/w1.bin"},
			{Name: "weight-2", Source: "w2.bin", Target: "/weights/w2.bin"},
		},
	}, tmpDir)

	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	// Build first weight
	spec1 := NewWeightSpec("weight-1", "w1.bin", "/weights/w1.bin")
	_, err = wb.Build(context.Background(), spec1)
	require.NoError(t, err)

	// Build second weight
	spec2 := NewWeightSpec("weight-2", "w2.bin", "/weights/w2.bin")
	_, err = wb.Build(context.Background(), spec2)
	require.NoError(t, err)

	// Lockfile should contain both entries
	lock, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Len(t, lock.Files, 2)

	names := map[string]bool{}
	for _, f := range lock.Files {
		names[f.Name] = true
	}
	require.True(t, names["weight-1"])
	require.True(t, names["weight-2"])
}

func TestWeightBuilder_CacheHit(t *testing.T) {
	// When a lockfile entry exists with matching name and size,
	// the builder should use the cached digest without re-hashing.
	tmpDir := t.TempDir()
	weightContent := []byte("cached weight data")
	err := os.WriteFile(filepath.Join(tmpDir, "model.bin"), weightContent, 0o644)
	require.NoError(t, err)

	hash := sha256.Sum256(weightContent)
	expectedDigest := "sha256:" + hex.EncodeToString(hash[:])

	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "my-model", Source: "model.bin", Target: "/weights/model.bin"},
		},
	}, tmpDir)

	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	// First build — populates lockfile
	spec := NewWeightSpec("my-model", "model.bin", "/weights/model.bin")
	artifact1, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	// Second build — should hit cache
	artifact2, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	// Both builds should produce the same digest
	wa1 := artifact1.(*WeightArtifact)
	wa2 := artifact2.(*WeightArtifact)
	require.Equal(t, expectedDigest, wa1.Descriptor().Digest.String())
	require.Equal(t, expectedDigest, wa2.Descriptor().Digest.String())

	// Lockfile should still have exactly one entry (not duplicated)
	lock, err := LoadWeightsLock(lockPath)
	require.NoError(t, err)
	require.Len(t, lock.Files, 1)
	require.Equal(t, "my-model", lock.Files[0].Name)
}

func TestWeightBuilder_CacheMiss_SizeChanged(t *testing.T) {
	// When the file size changes, the builder should re-hash.
	tmpDir := t.TempDir()
	content1 := []byte("original content")
	err := os.WriteFile(filepath.Join(tmpDir, "model.bin"), content1, 0o644)
	require.NoError(t, err)

	src := NewSourceFromConfig(&config.Config{
		Weights: []config.WeightSource{
			{Name: "my-model", Source: "model.bin", Target: "/weights/model.bin"},
		},
	}, tmpDir)

	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	spec := NewWeightSpec("my-model", "model.bin", "/weights/model.bin")

	// First build
	_, err = wb.Build(context.Background(), spec)
	require.NoError(t, err)

	// Change the file (different size)
	content2 := []byte("modified content with different size!!")
	err = os.WriteFile(filepath.Join(tmpDir, "model.bin"), content2, 0o644)
	require.NoError(t, err)

	// Second build — should detect size change and re-hash
	artifact2, err := wb.Build(context.Background(), spec)
	require.NoError(t, err)

	wa2 := artifact2.(*WeightArtifact)
	hash2 := sha256.Sum256(content2)
	expectedDigest2 := "sha256:" + hex.EncodeToString(hash2[:])
	require.Equal(t, expectedDigest2, wa2.Descriptor().Digest.String())
	require.Equal(t, int64(len(content2)), wa2.Descriptor().Size)
}

func TestWeightBuilder_ErrorWrongSpecType(t *testing.T) {
	tmpDir := t.TempDir()
	src := NewSourceFromConfig(&config.Config{}, tmpDir)
	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	// Pass an ImageSpec instead of WeightSpec
	imageSpec := NewImageSpec("model", "test-image")
	_, err := wb.Build(context.Background(), imageSpec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected *WeightSpec")
}

func TestWeightBuilder_ErrorFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	src := NewSourceFromConfig(&config.Config{}, tmpDir)
	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	spec := NewWeightSpec("missing", "nonexistent.bin", "/weights/nonexistent.bin")
	_, err := wb.Build(context.Background(), spec)
	require.Error(t, err)
	require.Contains(t, err.Error(), "weight source not found")
}

func TestWeightBuilder_ErrorContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "model.bin"), []byte("data"), 0o644)
	require.NoError(t, err)

	src := NewSourceFromConfig(&config.Config{}, tmpDir)
	lockPath := filepath.Join(tmpDir, "weights.lock")
	wb := NewWeightBuilder(src, "0.15.0", lockPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	spec := NewWeightSpec("model", "model.bin", "/weights/model.bin")
	_, err = wb.Build(ctx, spec)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWeightBuilder_ImplementsBuilderInterface(t *testing.T) {
	tmpDir := t.TempDir()
	src := NewSourceFromConfig(&config.Config{}, tmpDir)
	lockPath := filepath.Join(tmpDir, "weights.lock")

	// Compile-time check
	var _ Builder = NewWeightBuilder(src, "0.1.0", lockPath)
}
