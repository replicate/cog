package config

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

const testConfig = `
build:
  python_version: "3.8"
  python_requirements: requirements.txt
  system_packages:
    - libgl1-mesa-glx
    - libglib2.0-0
predict: "predict.py:SomePredictor"
`

func TestFindProjectRootDirShouldFindParentDir(t *testing.T) {
	projectDir := t.TempDir()

	err := os.WriteFile(path.Join(projectDir, "cog.yaml"), []byte(testConfig), 0o644)
	require.NoError(t, err)

	subdir := path.Join(projectDir, "some/sub/dir")
	err = os.MkdirAll(subdir, 0o700)
	require.NoError(t, err)

	foundDir, err := findProjectRootDir(subdir, "cog.yaml")
	require.NoError(t, err)
	require.Equal(t, foundDir, projectDir)
}

func TestFindProjectRootDirShouldReturnErrIfNoConfig(t *testing.T) {
	projectDir := t.TempDir()

	subdir := path.Join(projectDir, "some/sub/dir")
	err := os.MkdirAll(subdir, 0o700)
	require.NoError(t, err)

	_, err = findProjectRootDir(subdir, "cog.yaml")
	require.Error(t, err)
}
