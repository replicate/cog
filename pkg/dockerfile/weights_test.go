package dockerfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindWeights(t *testing.T) {
	folder := t.TempDir()
	tmpDir := t.TempDir()
	weights, err := FindWeights(folder, tmpDir)
	require.NoError(t, err)
	require.Empty(t, weights)
}
