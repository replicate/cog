package weights

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFindFastWeights(t *testing.T) {
	folder := t.TempDir()
	tmpDir := t.TempDir()
	weights, err := FindFastWeights(folder, tmpDir)
	require.NoError(t, err)
	require.Empty(t, weights)
}

func TestFindFastWeightsWithRemovedWeight(t *testing.T) {
	folder := t.TempDir()
	tmpDir := t.TempDir()
	weightFile := filepath.Join(tmpDir, WEIGHT_FILE)
	weights := []Weight{
		{
			Path:      "nonexistant_weight.h5",
			Digest:    "1",
			Timestamp: time.Now(),
			Size:      10,
		},
	}
	jsonData, err := json.MarshalIndent(weights, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(weightFile, jsonData, 0o644)
	require.NoError(t, err)
	weights, err = FindFastWeights(folder, tmpDir)
	require.NoError(t, err)
	require.Empty(t, weights)
}

func TestReadFastWeightsNoFile(t *testing.T) {
	dir := t.TempDir()
	weights, err := ReadFastWeights(dir)
	require.NoError(t, err)
	require.Empty(t, weights)
}

func TestFindFastWeightsWithDockerIgnore(t *testing.T) {
	folder := t.TempDir()

	weightFileName := "test_weight"
	weightFilePath := filepath.Join(folder, weightFileName)
	weightData := make([]byte, WEIGHT_FILE_SIZE_INCLUSION)
	err := os.WriteFile(weightFilePath, weightData, 0o644)
	require.NoError(t, err)

	dockerIgnoreFilePath := filepath.Join(folder, ".dockerignore")
	err = os.WriteFile(dockerIgnoreFilePath, []byte(weightFileName+"\n"), 0o644)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	weights, err := FindFastWeights(folder, tmpDir)
	require.NoError(t, err)
	require.Empty(t, weights)
}
