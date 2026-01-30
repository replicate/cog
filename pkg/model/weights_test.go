// pkg/model/weights_test.go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightsManifest(t *testing.T) {
	t.Run("total size calculation", func(t *testing.T) {
		wm := &WeightsManifest{
			Files: []WeightFile{
				{Size: 1000, SizeUncompressed: 2000},
				{Size: 500, SizeUncompressed: 1000},
			},
		}
		require.Equal(t, int64(1500), wm.TotalSize())
		require.Equal(t, int64(3000), wm.TotalSizeUncompressed())
	})

	t.Run("find file by dest", func(t *testing.T) {
		wm := &WeightsManifest{
			Files: []WeightFile{
				{Name: "model.safetensors", Dest: "/cache/model.safetensors"},
				{Name: "config.json", Dest: "/cache/config.json"},
			},
		}
		f := wm.FindByDest("/cache/config.json")
		require.NotNil(t, f)
		require.Equal(t, "config.json", f.Name)

		f = wm.FindByDest("/cache/notfound")
		require.Nil(t, f)
	})
}

func TestWeightFile(t *testing.T) {
	t.Run("media type constants", func(t *testing.T) {
		require.Equal(t, "application/vnd.cog.weights.layer.v1+gzip", MediaTypeWeightsLayerGzip)
		require.Equal(t, "application/vnd.cog.weights.v1", MediaTypeWeightsManifest)
	})
}
