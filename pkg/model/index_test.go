// pkg/model/index_test.go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelImageFormat(t *testing.T) {
	t.Run("standalone format", func(t *testing.T) {
		m := &Model{ImageFormat: FormatStandalone}
		require.False(t, m.IsBundle())
	})

	t.Run("bundle format", func(t *testing.T) {
		m := &Model{ImageFormat: FormatBundle}
		require.True(t, m.IsBundle())
	})

	t.Run("default format is standalone", func(t *testing.T) {
		m := &Model{}
		require.False(t, m.IsBundle())
	})
}

func TestModelImageFormatEnum(t *testing.T) {
	t.Run("String()", func(t *testing.T) {
		require.Equal(t, "standalone", FormatStandalone.String())
		require.Equal(t, "bundle", FormatBundle.String())
	})

	t.Run("IsValid()", func(t *testing.T) {
		require.True(t, FormatStandalone.IsValid())
		require.True(t, FormatBundle.IsValid())
		require.False(t, ModelImageFormat("invalid").IsValid())
		require.False(t, ModelImageFormat("").IsValid())
	})
}

func TestImageFormatFromEnv(t *testing.T) {
	t.Run("returns bundle when COG_OCI_INDEX=1", func(t *testing.T) {
		t.Setenv("COG_OCI_INDEX", "1")
		require.Equal(t, FormatBundle, ImageFormatFromEnv())
	})

	t.Run("returns standalone when COG_OCI_INDEX is not set", func(t *testing.T) {
		t.Setenv("COG_OCI_INDEX", "")
		require.Equal(t, FormatStandalone, ImageFormatFromEnv())
	})

	t.Run("returns standalone when COG_OCI_INDEX is other value", func(t *testing.T) {
		t.Setenv("COG_OCI_INDEX", "0")
		require.Equal(t, FormatStandalone, ImageFormatFromEnv())
	})
}

func TestManifestType(t *testing.T) {
	require.Equal(t, ManifestType("image"), ManifestTypeImage)
	require.Equal(t, ManifestType("weights"), ManifestTypeWeights)
}
