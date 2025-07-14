package builder

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContextFromDirectory(t *testing.T) {
	// Create a temporary directory with test files
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	// Create context from directory
	ctx, err := NewContextFromDirectory("test-context", tempDir)
	require.NoError(t, err)
	defer ctx.Close()

	// Verify properties
	assert.Equal(t, "test-context", ctx.Name())
	assert.NotNil(t, ctx.FS())
	assert.False(t, ctx.needsCleanup) // Directory context doesn't need cleanup
}

func TestNewContextFromFS(t *testing.T) {
	// Create test filesystem
	testFS := fstest.MapFS{
		"test.txt": &fstest.MapFile{
			Data: []byte("test content"),
			Mode: 0644,
		},
		"subdir/nested.txt": &fstest.MapFile{
			Data: []byte("nested content"),
			Mode: 0644,
		},
	}

	// Create context from fs.FS
	ctx, err := NewContextFromFS("test-fs-context", testFS)
	require.NoError(t, err)
	defer ctx.Close()

	// Verify properties
	assert.Equal(t, "test-fs-context", ctx.Name())
	assert.NotNil(t, ctx.FS())
	assert.True(t, ctx.needsCleanup) // fs.FS context needs cleanup
	assert.NotEmpty(t, ctx.tempDir)

	// Verify files were written to temp directory
	assert.FileExists(t, filepath.Join(ctx.tempDir, "test.txt"))
	assert.FileExists(t, filepath.Join(ctx.tempDir, "subdir", "nested.txt"))

	// Verify file contents
	content, err := os.ReadFile(filepath.Join(ctx.tempDir, "test.txt"))
	require.NoError(t, err)
	assert.Equal(t, "test content", string(content))
}

func TestNewWheelContextFS(t *testing.T) {
	// Test creating wheel context using generic context from temp directory
	tempDir := t.TempDir()

	// Create a fake wheel file in temp directory
	wheelFile := filepath.Join(tempDir, "cog-test-1.0.0-py3-none-any.whl")
	err := os.WriteFile(wheelFile, []byte("fake wheel content"), 0644)
	require.NoError(t, err)

	// Create context from directory
	ctx, err := NewContextFromDirectory("wheel-context", tempDir)
	require.NoError(t, err)
	defer ctx.Close()

	// Verify properties
	assert.Equal(t, "wheel-context", ctx.Name())
	assert.NotNil(t, ctx.FS())
	assert.False(t, ctx.needsCleanup) // Directory context doesn't need cleanup

	// Verify wheel file exists in directory
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Contains(t, files[0].Name(), ".whl")
}

func TestContextFS_Close(t *testing.T) {
	// Test directory context (no cleanup needed)
	tempDir := t.TempDir()
	dirCtx, err := NewContextFromDirectory("test", tempDir)
	require.NoError(t, err)

	err = dirCtx.Close()
	assert.NoError(t, err)
	assert.DirExists(t, tempDir) // Directory should still exist

	// Test fs.FS context (cleanup needed)
	testFS := fstest.MapFS{
		"test.txt": &fstest.MapFile{Data: []byte("test"), Mode: 0644},
	}

	fsCtx, err := NewContextFromFS("test", testFS)
	require.NoError(t, err)

	tempDir = fsCtx.tempDir
	assert.DirExists(t, tempDir)

	err = fsCtx.Close()
	assert.NoError(t, err)
	assert.NoDirExists(t, tempDir) // Temp directory should be cleaned up
}
