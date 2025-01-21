package dockerfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFindWeights(t *testing.T) {
	folder := t.TempDir()
	tmpDir := t.TempDir()
	weights, err := FindWeights(folder, tmpDir)
	require.NoError(t, err)
	require.Empty(t, weights)
}

func TestFindWeightsWithRemovedWeight(t *testing.T) {
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
	weights, err = FindWeights(folder, tmpDir)
	require.NoError(t, err)
	require.Empty(t, weights)
}

func TestReadWeightsNoFile(t *testing.T) {
	dir := t.TempDir()
	weights, err := ReadWeights(dir)
	require.NoError(t, err)
	require.Empty(t, weights)
}
