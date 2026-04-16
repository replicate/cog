package runner

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeSubpathAllowsPathInsideRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join("models", "fixture-a")

	path, err := safeSubpath(root, inside)
	require.NoError(t, err)

	rootAbs, err := filepath.Abs(root)
	require.NoError(t, err)
	assert.Contains(t, path, rootAbs)
	assert.Equal(t, filepath.Join(rootAbs, inside), path)
}

func TestSafeSubpathRejectsTraversal(t *testing.T) {
	root := t.TempDir()

	_, err := safeSubpath(root, "../outside")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes fixtures root")
}

func TestSafeSubpathRejectsAbsoluteOutsidePath(t *testing.T) {
	root := t.TempDir()
	absOutside := filepath.Dir(root)

	_, err := safeSubpath(root, absOutside)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be relative")
}

func TestJsonDiffUnifiedFormat(t *testing.T) {
	tests := []struct {
		name     string
		static   map[string]any
		runtime  map[string]any
		contains []string // substrings the diff must contain
		empty    bool     // expect no diff
	}{
		{
			name:    "identical schemas produce no diff",
			static:  map[string]any{"a": 1, "b": "hello"},
			runtime: map[string]any{"a": 1, "b": "hello"},
			empty:   true,
		},
		{
			name:     "missing key in static shows + lines",
			static:   map[string]any{"a": 1},
			runtime:  map[string]any{"a": 1, "b": "new"},
			contains: []string{"--- static schema", "+++ runtime schema", "@@ $.b @@", "+\"new\""},
		},
		{
			name:     "missing key in runtime shows - lines",
			static:   map[string]any{"a": 1, "b": "old"},
			runtime:  map[string]any{"a": 1},
			contains: []string{"@@ $.b @@", "-\"old\""},
		},
		{
			name:     "changed value shows - and + lines",
			static:   map[string]any{"a": "foo"},
			runtime:  map[string]any{"a": "bar"},
			contains: []string{"@@ $.a @@", "-\"foo\"", "+\"bar\""},
		},
		{
			name:     "nested object diff shows full path",
			static:   map[string]any{"outer": map[string]any{"inner": 1}},
			runtime:  map[string]any{"outer": map[string]any{"inner": 2}},
			contains: []string{"@@ $.outer.inner @@", "-1", "+2"},
		},
		{
			name:     "array length mismatch shows both arrays",
			static:   map[string]any{"arr": []any{"a", "b"}},
			runtime:  map[string]any{"arr": []any{"a", "b", "c"}},
			contains: []string{"@@ $.arr @@", "-[", "+[", "+  \"c\""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := jsonDiff(tt.static, tt.runtime)
			if tt.empty {
				assert.Empty(t, diff)
				return
			}
			require.NotEmpty(t, diff)
			for _, substr := range tt.contains {
				assert.True(t, strings.Contains(diff, substr),
					"diff should contain %q but got:\n%s", substr, diff)
			}
		})
	}
}

func TestJsonCompareClassifiesExpectedDiffs(t *testing.T) {
	t.Run("training schemas missing in runtime are expected", func(t *testing.T) {
		static := map[string]any{
			"components": map[string]any{
				"schemas": map[string]any{
					"Input":         map[string]any{"type": "object"},
					"TrainingInput": map[string]any{"type": "object"},
				},
			},
			"paths": map[string]any{
				"/predictions":                    map[string]any{"post": true},
				"/trainings":                      map[string]any{"post": true},
				"/trainings/{training_id}/cancel": map[string]any{"post": true},
			},
		}
		runtime := map[string]any{
			"components": map[string]any{
				"schemas": map[string]any{
					"Input": map[string]any{"type": "object"},
				},
			},
			"paths": map[string]any{
				"/predictions": map[string]any{"post": true},
			},
		}

		result := jsonCompare(static, runtime)
		assert.Empty(t, result.Real, "training schema diffs should not be real failures")
		assert.Len(t, result.Expected, 3, "should have 3 expected diffs (TrainingInput, /trainings, /trainings/.../cancel)")
	})

	t.Run("missing description in static is expected", func(t *testing.T) {
		static := map[string]any{
			"properties": map[string]any{
				"steps": map[string]any{
					"type":    "integer",
					"default": float64(4),
				},
			},
		}
		runtime := map[string]any{
			"properties": map[string]any{
				"steps": map[string]any{
					"type":        "integer",
					"default":     float64(4),
					"description": "Number of denoising steps.",
				},
			},
		}

		result := jsonCompare(static, runtime)
		assert.Empty(t, result.Real, "missing description in static should not be a real failure")
		assert.Len(t, result.Expected, 1)
	})

	t.Run("real diffs are not classified as expected", func(t *testing.T) {
		static := map[string]any{
			"type":    "integer",
			"default": float64(4),
		}
		runtime := map[string]any{
			"type":    "string",
			"default": float64(4),
		}

		result := jsonCompare(static, runtime)
		assert.Len(t, result.Real, 1, "type mismatch should be a real failure")
		assert.Empty(t, result.Expected)
	})

	t.Run("only expected diffs means jsonDiff returns empty", func(t *testing.T) {
		static := map[string]any{
			"components": map[string]any{
				"schemas": map[string]any{
					"TrainingInput": map[string]any{"type": "object"},
				},
			},
		}
		runtime := map[string]any{
			"components": map[string]any{
				"schemas": map[string]any{},
			},
		}

		diff := jsonDiff(static, runtime)
		assert.Empty(t, diff, "jsonDiff should return empty when only expected diffs exist")
	})
}

func TestExtractOutput(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		stderr   string
		modelDir string
		want     string
	}{
		{
			name:   "stdout only",
			stdout: "hello world\n",
			stderr: "Building...\nRunning prediction...\n",
			want:   "hello world",
		},
		{
			name:   "stdout with trailing whitespace",
			stdout: "  result  \n",
			stderr: "",
			want:   "result",
		},
		{
			name:     "file output on stderr with relative path",
			stdout:   "",
			stderr:   "Building...\nWritten output to: output.png\n",
			modelDir: "/tmp/model",
			want:     "/tmp/model/output.png",
		},
		{
			name:     "file output on stderr with absolute path",
			stdout:   "",
			stderr:   "Written output to: /abs/path/output.png\n",
			modelDir: "/tmp/model",
			want:     "/abs/path/output.png",
		},
		{
			name:     "file output takes precedence over stdout",
			stdout:   "some text output",
			stderr:   "Written output to: output.png\n",
			modelDir: "/tmp/model",
			want:     "/tmp/model/output.png",
		},
		{
			name:   "empty stdout and stderr",
			stdout: "",
			stderr: "",
			want:   "",
		},
		{
			name:   "stdout with build noise on stderr",
			stdout: "42\n",
			stderr: "#1 [internal] load build definition\n#2 DONE 0.0s\nStarting Docker image...\nRunning prediction...\n",
			want:   "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOutput(tt.stdout, tt.stderr, tt.modelDir)
			assert.Equal(t, tt.want, got)
		})
	}
}
