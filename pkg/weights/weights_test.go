package weights

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockFileInfo is a test type to mock os.FileInfo
type mockFileInfo struct {
	size int64
}

func (mfi mockFileInfo) Size() int64 {
	return mfi.size
}
func (mfi mockFileInfo) Name() string {
	return ""
}
func (mfi mockFileInfo) Mode() os.FileMode {
	return 0
}
func (mfi mockFileInfo) ModTime() time.Time {
	return time.Time{}
}
func (mfi mockFileInfo) IsDir() bool {
	return false
}
func (mfi mockFileInfo) Sys() interface{} {
	return nil
}

// Test case for root directory with large and small model files
func TestRootDirModelFiles(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, sizeThreshold, sizeThreshold - 1}
		for i, path := range []string{"large-a", "large-b", "small"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Equal(t, []string{"large-a", "large-b"}, rootFiles)
	require.Empty(t, dirs)
}

// Test case for sub directory with large and small model files
func TestSubDirModelFiles(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, sizeThreshold, sizeThreshold - 1}
		for i, path := range []string{"models/large-a", "models/large-b", "models/small"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Empty(t, rootFiles)
	require.Equal(t, []string{"models"}, dirs)
}

// Test case for both root and sub directory with large model files
func TestRootAndSubDirModelFiles(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, sizeThreshold}
		for i, path := range []string{"root-large", "models/large-a"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Equal(t, []string{"root-large"}, rootFiles)
	require.Equal(t, []string{"models"}, dirs)
}

// Test case for root directory with both large model and code files
func TestRootDirLargeModelAndCodeFiles(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, 1024}
		for i, path := range []string{"root-large", "predict.py"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Equal(t, []string{"root-large"}, rootFiles)
	require.Empty(t, dirs)
}

// Test case for sub directory with both large model and code files
func TestSubDirLargeModelAndCodeFiles(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, 1024}
		for i, path := range []string{"models/root-large", "models/predict.py"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Empty(t, rootFiles)
	require.Empty(t, dirs)
}

// Test case for sub-directory with code files under large model directory
func TestSubDirLargeModelDirWithCodeFiles(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, 1024}
		for i, path := range []string{"models/root-large", "models/code/predict.py"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Empty(t, rootFiles)
	require.Empty(t, dirs)
}

// Test case for sorting for model directories
func TestDirSorting(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, sizeThreshold, sizeThreshold}
		for i, path := range []string{"models2/b/large", "models2/a/large", "models/large"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Empty(t, rootFiles)
	require.Equal(t, []string{"models", "models2/a", "models2/b"}, dirs)
}

// Test case for merging sub-directories with large models
func TestSubDirMerge(t *testing.T) {
	mockFileWalker := func(root string, walkFn filepath.WalkFunc) error {
		sizes := []int64{sizeThreshold, sizeThreshold, sizeThreshold}
		for i, path := range []string{"models/b/large", "models/a/large", "models/large"} {
			walkFn(path, mockFileInfo{size: sizes[i]}, nil)
		}
		return nil
	}

	dirs, rootFiles, err := FindWeights(mockFileWalker)
	require.NoError(t, err)
	require.Empty(t, rootFiles)
	require.Equal(t, []string{"models"}, dirs)
}
