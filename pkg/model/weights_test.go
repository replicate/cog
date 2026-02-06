package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightFile(t *testing.T) {
	t.Run("media type constants", func(t *testing.T) {
		require.Equal(t, "application/vnd.cog.weight.layer.v1+gzip", MediaTypeWeightLayerGzip)
		require.Equal(t, "application/vnd.cog.weight.v1", MediaTypeWeightArtifact)
		require.Equal(t, "application/vnd.cog.weight.layer.v1", MediaTypeWeightLayer)
	})
}
