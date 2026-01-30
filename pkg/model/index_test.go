// pkg/model/index_test.go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelFormat(t *testing.T) {
	t.Run("image format", func(t *testing.T) {
		m := &Model{Format: ModelFormatImage}
		require.False(t, m.IsIndexed())
	})

	t.Run("index format", func(t *testing.T) {
		m := &Model{Format: ModelFormatIndex}
		require.True(t, m.IsIndexed())
	})

	t.Run("default format is image", func(t *testing.T) {
		m := &Model{}
		require.False(t, m.IsIndexed())
	})
}

func TestManifestType(t *testing.T) {
	require.Equal(t, ManifestType("image"), ManifestTypeImage)
	require.Equal(t, ManifestType("weights"), ManifestTypeWeights)
}
