package model

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/model/weightsource"
)

// Plan tests operate purely on Inventory — no disk, no source. They
// exercise layer-assignment logic (classification, bundling, media-type
// selection) that the Execute tests only observe indirectly through tar
// output.

func invFile(path string, size int64) weightsource.InventoryFile {
	return weightsource.InventoryFile{Path: path, Size: size, Digest: "sha256:deadbeef"}
}

func TestPacker_Plan_EmptyInventory(t *testing.T) {
	plan := NewPacker(nil).Plan(weightsource.Inventory{})
	assert.Empty(t, plan.Layers, "empty inventory should plan zero layers")
}

func TestPacker_Plan_SingleSmallFile(t *testing.T) {
	plan := NewPacker(nil).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{invFile("config.json", 100)},
	})
	require.Len(t, plan.Layers, 1)
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), plan.Layers[0].MediaType,
		"small-file bundle should be gzipped")
	require.Len(t, plan.Layers[0].Files, 1)
	assert.Equal(t, "config.json", plan.Layers[0].Files[0].Path)
}

func TestPacker_Plan_SingleLargeFileIncompressible(t *testing.T) {
	plan := NewPacker(nil).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{invFile("model.safetensors", 100*1024*1024)},
	})
	require.Len(t, plan.Layers, 1)
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTar), plan.Layers[0].MediaType,
		".safetensors should be uncompressed")
}

func TestPacker_Plan_SingleLargeFileCompressible(t *testing.T) {
	plan := NewPacker(nil).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{invFile("model.dat", 100*1024*1024)},
	})
	require.Len(t, plan.Layers, 1)
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), plan.Layers[0].MediaType,
		".dat is not in the incompressible set")
}

func TestPacker_Plan_MixedFilesOrdering(t *testing.T) {
	// Small files arrive in unsorted order; the planner must sort
	// them within the bundle for deterministic output. Large files
	// follow in the order they appear in the inventory.
	plan := NewPacker(nil).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{
			invFile("z-small.json", 100),
			invFile("large-01.safetensors", 100*1024*1024),
			invFile("a-small.json", 100),
			invFile("large-02.safetensors", 100*1024*1024),
		},
	})
	require.Len(t, plan.Layers, 3, "1 bundle + 2 large files")

	// Bundle layer first, small files sorted by path.
	require.Len(t, plan.Layers[0].Files, 2)
	assert.Equal(t, "a-small.json", plan.Layers[0].Files[0].Path)
	assert.Equal(t, "z-small.json", plan.Layers[0].Files[1].Path)

	// Large files in inventory order.
	assert.Equal(t, "large-01.safetensors", plan.Layers[1].Files[0].Path)
	assert.Equal(t, "large-02.safetensors", plan.Layers[2].Files[0].Path)
}

func TestPacker_Plan_BundleSplitsOnSizeMax(t *testing.T) {
	// Everything is small (under BundleFileMax=1024), but bundle
	// size is capped at 20 bytes. Expect a+b in one bundle, c in
	// another.
	plan := NewPacker(&PackOptions{
		BundleFileMax: 1024,
		BundleSizeMax: 20,
	}).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{
			invFile("a.txt", 10),
			invFile("b.txt", 10),
			invFile("c.txt", 10),
		},
	})
	require.Len(t, plan.Layers, 2)
	require.Len(t, plan.Layers[0].Files, 2)
	assert.Equal(t, "a.txt", plan.Layers[0].Files[0].Path)
	assert.Equal(t, "b.txt", plan.Layers[0].Files[1].Path)
	require.Len(t, plan.Layers[1].Files, 1)
	assert.Equal(t, "c.txt", plan.Layers[1].Files[0].Path)
}

func TestPacker_Plan_FileAtExactThreshold(t *testing.T) {
	// A file equal to BundleFileMax is "large" (strict less-than).
	plan := NewPacker(nil).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{invFile("model.bin", DefaultBundleFileMax)},
	})
	require.Len(t, plan.Layers, 1)
	require.Len(t, plan.Layers[0].Files, 1)
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTar), plan.Layers[0].MediaType,
		"at-threshold large file should not be bundled")
}

func TestPacker_Plan_FileJustBelowThreshold(t *testing.T) {
	plan := NewPacker(nil).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{invFile("model.bin", DefaultBundleFileMax-1)},
	})
	require.Len(t, plan.Layers, 1)
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), plan.Layers[0].MediaType,
		"below-threshold file should land in a bundle")
}

func TestPacker_Plan_IncompressibleExtensions(t *testing.T) {
	tests := []struct {
		ext       string
		mediaType types.MediaType
	}{
		{".safetensors", MediaTypeOCILayerTar},
		{".bin", MediaTypeOCILayerTar},
		{".gguf", MediaTypeOCILayerTar},
		{".onnx", MediaTypeOCILayerTar},
		{".parquet", MediaTypeOCILayerTar},
		{".pt", MediaTypeOCILayerTar},
		{".pth", MediaTypeOCILayerTar},
		{".dat", MediaTypeOCILayerTarGzip},
		{".json", MediaTypeOCILayerTarGzip},
		{".pickle", MediaTypeOCILayerTarGzip},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			plan := NewPacker(nil).Plan(weightsource.Inventory{
				Files: []weightsource.InventoryFile{invFile("model"+tt.ext, 100*1024*1024)},
			})
			require.Len(t, plan.Layers, 1)
			assert.Equal(t, tt.mediaType, plan.Layers[0].MediaType)
		})
	}
}

func TestPacker_Plan_SingleLargeFileExceedingBundleSizeMax(t *testing.T) {
	// A small-classified file that still exceeds bundleMax gets its
	// own bundle (the flush-before-add guard skips when currentSize
	// is 0). This is unusual in practice but the documented behavior.
	plan := NewPacker(&PackOptions{
		BundleFileMax: 1024,
		BundleSizeMax: 20,
	}).Plan(weightsource.Inventory{
		Files: []weightsource.InventoryFile{invFile("big-small.txt", 100)},
	})
	require.Len(t, plan.Layers, 1)
	assert.Equal(t, types.MediaType(MediaTypeOCILayerTarGzip), plan.Layers[0].MediaType)
	require.Len(t, plan.Layers[0].Files, 1)
	assert.Equal(t, "big-small.txt", plan.Layers[0].Files[0].Path)
}

func TestPacker_Plan_Deterministic(t *testing.T) {
	// Same inventory → identical Plan, run twice.
	inv := weightsource.Inventory{
		Files: []weightsource.InventoryFile{
			invFile("b/c.json", 100),
			invFile("a.json", 200),
			invFile("model.safetensors", 100*1024*1024),
		},
	}
	p := NewPacker(nil)
	plan1 := p.Plan(inv)
	plan2 := p.Plan(inv)
	assert.Equal(t, plan1, plan2)
}

func TestPacker_Execute_RejectsEmptyPlan(t *testing.T) {
	_, err := NewPacker(nil).Execute(t.Context(), nil, Plan{})
	assert.ErrorContains(t, err, "no layers in plan")
}
