package runner

import (
	"path/filepath"
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
