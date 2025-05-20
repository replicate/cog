package dockerignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalk(t *testing.T) {
	dir := t.TempDir()

	predictOtherPyFilename := "predict_other.py"
	predictOtherPyFilepath := filepath.Join(dir, predictOtherPyFilename)
	predictOtherPyHandle, err := os.Create(predictOtherPyFilepath)
	require.NoError(t, err)
	predictOtherPyHandle.WriteString("import cog")

	dockerIgnorePath := filepath.Join(dir, ".dockerignore")
	dockerIgnoreHandle, err := os.Create(dockerIgnorePath)
	require.NoError(t, err)
	dockerIgnoreHandle.WriteString(predictOtherPyFilename)

	predictPyFilename := "predict.py"
	predictPyFilepath := filepath.Join(dir, predictPyFilename)
	predictPyHandle, err := os.Create(predictPyFilepath)
	require.NoError(t, err)
	predictPyHandle.WriteString("import cog")

	matcher, err := CreateMatcher(dir)
	require.NoError(t, err)

	foundFiles := []string{}
	err = Walk(dir, matcher, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		foundFiles = append(foundFiles, relPath)

		return nil
	})
	require.NoError(t, err)

	require.Equal(t, []string{predictPyFilename}, foundFiles)
}
