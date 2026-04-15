package patcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatch(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("patch sdk version", func(t *testing.T) {
		cogYAML := filepath.Join(tmpDir, "cog1.yaml")
		content := `build:
  python_version: "3.10"
predict: predict.py
`
		require.NoError(t, os.WriteFile(cogYAML, []byte(content), 0o644))
		require.NoError(t, Patch(cogYAML, "0.16.12", nil))

		data, err := os.ReadFile(cogYAML)
		require.NoError(t, err)
		assert.Contains(t, string(data), "sdk_version: 0.16.12")
	})

	t.Run("patch with overrides", func(t *testing.T) {
		cogYAML := filepath.Join(tmpDir, "cog2.yaml")
		content := `build:
  python_version: "3.10"
predict: predict.py
`
		require.NoError(t, os.WriteFile(cogYAML, []byte(content), 0o644))

		overrides := map[string]any{
			"build": map[string]any{
				"system_packages": []string{"ffmpeg"},
			},
		}

		require.NoError(t, Patch(cogYAML, "", overrides))

		data, err := os.ReadFile(cogYAML)
		require.NoError(t, err)
		assert.Contains(t, string(data), "system_packages")
	})

	t.Run("patch sdk and overrides", func(t *testing.T) {
		cogYAML := filepath.Join(tmpDir, "cog3.yaml")
		content := `build:
  python_version: "3.10"
predict: predict.py
`
		require.NoError(t, os.WriteFile(cogYAML, []byte(content), 0o644))

		overrides := map[string]any{
			"predict": "new_predict.py",
		}

		require.NoError(t, Patch(cogYAML, "0.16.12", overrides))

		data, err := os.ReadFile(cogYAML)
		require.NoError(t, err)

		result := string(data)
		assert.Contains(t, result, "sdk_version: 0.16.12")
		assert.Contains(t, result, "new_predict.py")
	})
}

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]any
		override map[string]any
		want     map[string]any
	}{
		{
			name:     "simple merge",
			base:     map[string]any{"a": 1},
			override: map[string]any{"b": 2},
			want:     map[string]any{"a": 1, "b": 2},
		},
		{
			name:     "nested merge",
			base:     map[string]any{"build": map[string]any{"python": "3.10"}},
			override: map[string]any{"build": map[string]any{"sdk": "0.16"}},
			want:     map[string]any{"build": map[string]any{"python": "3.10", "sdk": "0.16"}},
		},
		{
			name:     "override replaces",
			base:     map[string]any{"a": 1},
			override: map[string]any{"a": 2},
			want:     map[string]any{"a": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, deepMerge(tt.base, tt.override))
		})
	}
}
