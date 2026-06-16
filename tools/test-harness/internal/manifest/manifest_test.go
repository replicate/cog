package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestLoadRejectsRepoLocal(t *testing.T) {
	path := writeManifest(t, `
models:
  - name: legacy
    repo: local
    path: legacy
`)

	_, _, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'repo: local' is no longer supported")
	assert.Contains(t, err.Error(), "legacy")
}

func TestLoadAcceptsBaseDirAndRepo(t *testing.T) {
	path := writeManifest(t, `
models:
  - name: example
    base_dir: examples
    path: hello-world
  - name: remote
    repo: replicate/cog-examples
    path: hello-world
  - name: fixture
    base_dir: tools/test-harness/fixtures/models
    path: scalar-types
`)

	mf, _, err := Load(path)
	require.NoError(t, err)
	require.Len(t, mf.Models, 3)
	// Default timeout applied.
	assert.Equal(t, 300, mf.Models[0].Timeout)
}

func TestModelSource(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		want  string
	}{
		{
			name:  "base_dir local",
			model: Model{BaseDir: "examples", Path: "hello-world"},
			want:  "examples/hello-world",
		},
		{
			name:  "repo clone",
			model: Model{Repo: "replicate/cog-examples", Path: "hello-world"},
			want:  "replicate/cog-examples/hello-world",
		},
		{
			name:  "default fixtures",
			model: Model{Path: "scalar-types"},
			want:  "fixtures/models/scalar-types",
		},
		{
			name:  "base_dir takes priority over repo",
			model: Model{BaseDir: "examples", Repo: "replicate/cog-examples", Path: "hello-world"},
			want:  "examples/hello-world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.model.Source())
		})
	}
}
