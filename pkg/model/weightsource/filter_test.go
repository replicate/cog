package weightsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testInventory builds an Inventory with the given paths (all other fields
// are placeholders — FilterInventory only inspects Path).
func testInventory(paths ...string) Inventory {
	files := make([]InventoryFile, len(paths))
	for i, p := range paths {
		files[i] = InventoryFile{Path: p, Size: 100, Digest: "sha256:deadbeef"}
	}
	return Inventory{
		Files:       files,
		Fingerprint: Fingerprint("test:abc123"),
	}
}

func filePaths(inv Inventory) []string {
	paths := make([]string, len(inv.Files))
	for i, f := range inv.Files {
		paths[i] = f.Path
	}
	return paths
}

func TestFilterInventory(t *testing.T) {
	allFiles := testInventory(
		".gitattributes",
		"README.md",
		"config.json",
		"model.safetensors",
		"pytorch_model.bin",
		"onnx/model.onnx",
		"onnx/model_O1.onnx",
		"openvino/openvino_model.bin",
		"openvino/openvino_model.xml",
		"tokenizer.json",
		"tokenizer_config.json",
		"deep/nested/dir/weights.safetensors",
	)

	tests := []struct {
		name      string
		inv       Inventory
		include   []string
		exclude   []string
		wantPaths []string
		wantErr   string
	}{
		{
			name:      "no patterns passes all files",
			inv:       allFiles,
			wantPaths: filePaths(allFiles),
		},
		{
			name:      "nil patterns passes all files",
			inv:       allFiles,
			include:   nil,
			exclude:   nil,
			wantPaths: filePaths(allFiles),
		},
		{
			name:      "empty slices passes all files",
			inv:       allFiles,
			include:   []string{},
			exclude:   []string{},
			wantPaths: filePaths(allFiles),
		},
		{
			name:    "include safetensors and json",
			inv:     allFiles,
			include: []string{"*.safetensors", "*.json"},
			wantPaths: []string{
				"config.json",
				"model.safetensors",
				"tokenizer.json",
				"tokenizer_config.json",
				"deep/nested/dir/weights.safetensors",
			},
		},
		{
			name:    "exclude onnx and bin",
			inv:     allFiles,
			exclude: []string{"*.onnx", "*.bin"},
			wantPaths: []string{
				".gitattributes",
				"README.md",
				"config.json",
				"model.safetensors",
				"openvino/openvino_model.xml",
				"tokenizer.json",
				"tokenizer_config.json",
				"deep/nested/dir/weights.safetensors",
			},
		},
		{
			name:    "include and exclude together",
			inv:     allFiles,
			include: []string{"*.safetensors", "*.json", "*.bin"},
			exclude: []string{"*.bin"},
			wantPaths: []string{
				"config.json",
				"model.safetensors",
				"tokenizer.json",
				"tokenizer_config.json",
				"deep/nested/dir/weights.safetensors",
			},
		},
		{
			name:    "exclude takes precedence over include",
			inv:     allFiles,
			include: []string{"*.bin"},
			exclude: []string{"*.bin"},
			wantErr: "matched zero files",
		},
		{
			name:    "anchored path pattern",
			inv:     allFiles,
			exclude: []string{"onnx/*"},
			wantPaths: []string{
				".gitattributes",
				"README.md",
				"config.json",
				"model.safetensors",
				"pytorch_model.bin",
				"openvino/openvino_model.bin",
				"openvino/openvino_model.xml",
				"tokenizer.json",
				"tokenizer_config.json",
				"deep/nested/dir/weights.safetensors",
			},
		},
		{
			name:    "double-star recursion in include",
			inv:     allFiles,
			include: []string{"**/*.safetensors"},
			wantPaths: []string{
				"model.safetensors",
				"deep/nested/dir/weights.safetensors",
			},
		},
		{
			name:    "directory pattern excludes subtree",
			inv:     allFiles,
			exclude: []string{"openvino/"},
			wantPaths: []string{
				".gitattributes",
				"README.md",
				"config.json",
				"model.safetensors",
				"pytorch_model.bin",
				"onnx/model.onnx",
				"onnx/model_O1.onnx",
				"tokenizer.json",
				"tokenizer_config.json",
				"deep/nested/dir/weights.safetensors",
			},
		},
		{
			name:    "case sensitive matching",
			inv:     testInventory("Model.Safetensors", "model.safetensors"),
			include: []string{"*.safetensors"},
			wantPaths: []string{
				"model.safetensors",
			},
		},
		{
			name:    "zero survivors from include mismatch",
			inv:     testInventory("model.safetensors", "config.json"),
			include: []string{"*.gguf"},
			wantErr: "matched zero files out of 2",
		},
		{
			name:    "zero survivors from total exclude",
			inv:     testInventory("a.bin"),
			exclude: []string{"*.bin"},
			wantErr: "matched zero files out of 1",
		},
		{
			name:      "empty inventory with no patterns is ok",
			inv:       testInventory(),
			include:   nil,
			exclude:   nil,
			wantPaths: []string{},
		},
		{
			name:    "single include pattern",
			inv:     allFiles,
			include: []string{"config.json"},
			wantPaths: []string{
				"config.json",
			},
		},
		{
			name:    "forward slashes only",
			inv:     testInventory("a/b/c.bin", "a/b/d.safetensors"),
			include: []string{"a/b/*.safetensors"},
			wantPaths: []string{
				"a/b/d.safetensors",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FilterInventory(tt.inv, tt.include, tt.exclude)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				// Verify it's a ZeroSurvivorsError
				var zse *ZeroSurvivorsError
				assert.ErrorAs(t, err, &zse)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPaths, filePaths(result))

			// Fingerprint must be preserved from original.
			assert.Equal(t, tt.inv.Fingerprint, result.Fingerprint)
		})
	}
}

func TestZeroSurvivorsError_Message(t *testing.T) {
	err := &ZeroSurvivorsError{
		InventorySize: 42,
		Include:       []string{"*.safetensors"},
		Exclude:       []string{"*.bin", "*.onnx"},
	}
	msg := err.Error()
	assert.Contains(t, msg, "42")
	assert.Contains(t, msg, "*.safetensors")
	assert.Contains(t, msg, "*.bin")
	assert.Contains(t, msg, "*.onnx")
	assert.Contains(t, msg, "check your patterns")
}
