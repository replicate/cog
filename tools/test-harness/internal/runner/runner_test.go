package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/tools/test-harness/internal/manifest"
)

func TestResolveLocalBaseDir(t *testing.T) {
	// Simulate a repo root (has .git) with the manifest nested under it.
	repoRoot := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755))
	manifestDir := filepath.Join(repoRoot, "tools", "test-harness")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))

	r := &Runner{
		fixturesDir: "/fixtures",
		manifestDir: manifestDir,
	}

	tests := []struct {
		name    string
		baseDir string
		want    string
	}{
		{
			name:    "no base_dir defaults to fixtures/models",
			baseDir: "",
			want:    filepath.Join("/fixtures", "models"),
		},
		{
			name:    "relative base_dir resolves against repo root",
			baseDir: "examples",
			want:    filepath.Join(repoRoot, "examples"),
		},
		{
			name:    "nested relative base_dir resolves against repo root",
			baseDir: "tools/test-harness/fixtures/models",
			want:    filepath.Join(repoRoot, "tools", "test-harness", "fixtures", "models"),
		},
		{
			name:    "absolute base_dir is used as-is",
			baseDir: "/abs/examples",
			want:    "/abs/examples",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.resolveLocalBaseDir(manifest.Model{BaseDir: tt.baseDir})
			assert.Equal(t, tt.want, got)
		})
	}
}

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
