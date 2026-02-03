// pkg/model/weights_lock_generator_test.go
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestWeightsLockGenerator_ProcessSingleFile(t *testing.T) {
	// Create a temp directory with a single weight file
	tmpDir := t.TempDir()
	weightContent := []byte("test weight file content")
	weightFile := filepath.Join(tmpDir, "model.safetensors")
	err := os.WriteFile(weightFile, weightContent, 0o644)
	require.NoError(t, err)

	gen := NewWeightsLockGenerator(WeightsLockGeneratorOptions{
		DestPrefix: "/weights",
	})

	sources := []config.WeightSource{
		{Source: "model.safetensors"},
	}

	lock, err := gen.Generate(tmpDir, sources)
	require.NoError(t, err)
	require.NotNil(t, lock)

	require.Equal(t, "1.0", lock.Version)
	require.Len(t, lock.Files, 1)

	wf := lock.Files[0]
	require.Equal(t, "model", wf.Name)
	require.Equal(t, "/weights/model.safetensors", wf.Dest)
	require.Equal(t, int64(len(weightContent)), wf.Size)
	require.Equal(t, int64(len(weightContent)), wf.SizeUncompressed)
	require.Equal(t, MediaTypeWeightsLayer, wf.MediaType)
}

func TestWeightsLockGenerator_ProcessDirectory(t *testing.T) {
	// Create a temp directory with a subdirectory containing multiple files
	tmpDir := t.TempDir()
	weightsDir := filepath.Join(tmpDir, "weights")
	err := os.MkdirAll(weightsDir, 0o755)
	require.NoError(t, err)

	files := map[string][]byte{
		"model.bin":     []byte("model binary data"),
		"config.json":   []byte(`{"hidden_size": 768}`),
		"tokenizer.bin": []byte("tokenizer data"),
	}

	for name, content := range files {
		err := os.WriteFile(filepath.Join(weightsDir, name), content, 0o644)
		require.NoError(t, err)
	}

	gen := NewWeightsLockGenerator(WeightsLockGeneratorOptions{
		DestPrefix: "/cache",
	})

	sources := []config.WeightSource{
		{Source: "weights"},
	}

	lock, err := gen.Generate(tmpDir, sources)
	require.NoError(t, err)
	require.NotNil(t, lock)

	require.Len(t, lock.Files, 3)

	// Verify all files are present
	destPaths := make(map[string]bool)
	for _, wf := range lock.Files {
		destPaths[wf.Dest] = true
		require.Equal(t, MediaTypeWeightsLayer, wf.MediaType)
	}

	require.True(t, destPaths["/cache/weights/model.bin"])
	require.True(t, destPaths["/cache/weights/config.json"])
	require.True(t, destPaths["/cache/weights/tokenizer.bin"])
}

func TestWeightsLockGenerator_CustomTarget(t *testing.T) {
	tmpDir := t.TempDir()
	weightContent := []byte("custom target weight")
	weightFile := filepath.Join(tmpDir, "local-model.bin")
	err := os.WriteFile(weightFile, weightContent, 0o644)
	require.NoError(t, err)

	gen := NewWeightsLockGenerator(WeightsLockGeneratorOptions{
		DestPrefix: "/weights",
	})

	sources := []config.WeightSource{
		{Source: "local-model.bin", Target: "/custom/path/model.bin"},
	}

	lock, err := gen.Generate(tmpDir, sources)
	require.NoError(t, err)
	require.NotNil(t, lock)

	require.Len(t, lock.Files, 1)
	wf := lock.Files[0]
	// Custom target should override the default dest
	require.Equal(t, "/custom/path/model.bin", wf.Dest)
	require.Equal(t, "local-model", wf.Name)
}

func TestWeightsLockGenerator_MissingSource(t *testing.T) {
	tmpDir := t.TempDir()

	gen := NewWeightsLockGenerator(WeightsLockGeneratorOptions{
		DestPrefix: "/weights",
	})

	sources := []config.WeightSource{
		{Source: "nonexistent.bin"},
	}

	lock, err := gen.Generate(tmpDir, sources)
	require.Error(t, err)
	require.Nil(t, lock)
	require.Contains(t, err.Error(), "nonexistent.bin")
}

func TestWeightsLockGenerator_DigestCorrectness(t *testing.T) {
	tmpDir := t.TempDir()
	weightContent := []byte("verify digest correctness")
	weightFile := filepath.Join(tmpDir, "model.bin")
	err := os.WriteFile(weightFile, weightContent, 0o644)
	require.NoError(t, err)

	// Compute expected digest
	hash := sha256.Sum256(weightContent)
	expectedDigest := "sha256:" + hex.EncodeToString(hash[:])

	gen := NewWeightsLockGenerator(WeightsLockGeneratorOptions{
		DestPrefix: "/weights",
	})

	sources := []config.WeightSource{
		{Source: "model.bin"},
	}

	lock, err := gen.Generate(tmpDir, sources)
	require.NoError(t, err)
	require.NotNil(t, lock)

	require.Len(t, lock.Files, 1)
	wf := lock.Files[0]
	require.Equal(t, expectedDigest, wf.Digest)
	require.Equal(t, expectedDigest, wf.DigestOriginal)
}

func TestWeightsLockGenerator_FilePaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file and a directory with files
	err := os.WriteFile(filepath.Join(tmpDir, "single.bin"), []byte("single"), 0o644)
	require.NoError(t, err)

	subDir := filepath.Join(tmpDir, "subdir")
	err = os.MkdirAll(subDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(subDir, "nested.bin"), []byte("nested"), 0o644)
	require.NoError(t, err)

	gen := NewWeightsLockGenerator(WeightsLockGeneratorOptions{
		DestPrefix: "/weights",
	})

	sources := []config.WeightSource{
		{Source: "single.bin"},
		{Source: "subdir"},
	}

	lock, filePaths, err := gen.GenerateWithFilePaths(tmpDir, sources)
	require.NoError(t, err)
	require.NotNil(t, lock)
	require.NotNil(t, filePaths)

	require.Len(t, lock.Files, 2)
	require.Len(t, filePaths, 2)

	// Verify file paths map weight names to absolute paths
	singlePath, ok := filePaths["single"]
	require.True(t, ok, "single should be in filePaths")
	require.Equal(t, filepath.Join(tmpDir, "single.bin"), singlePath)

	nestedPath, ok := filePaths["nested"]
	require.True(t, ok, "nested should be in filePaths")
	require.Equal(t, filepath.Join(subDir, "nested.bin"), nestedPath)
}
