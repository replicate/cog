package config

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

const TEST_CONFIG = `
environment:
  python_version: "3.8"
  python_requirements: requirements.txt
  system_packages:
    - libgl1-mesa-glx
    - libglib2.0-0
predict: "predict.py:SomePredictor"
`

func TestGetProjectDirWithFlagSet(t *testing.T) {
	projectDirFlag := "foo"

	projectDir, err := GetProjectDir(projectDirFlag)
	require.NoError(t, err)
	require.Equal(t, projectDir, projectDirFlag)
}

func TestGetConfigShouldLoadFromCustomDir(t *testing.T) {
	dir, err := ioutil.TempDir("", "cog-test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = ioutil.WriteFile(path.Join(dir, "cog.yaml"), []byte(TEST_CONFIG), 0644)
	require.NoError(t, err)
	conf, _, err := GetConfig(dir)
	require.NoError(t, err)
	want := &Config{
		Predict: "predict.py:SomePredictor",
		Environment: &Environment{
			PythonVersion:      "3.8",
			PythonRequirements: "requirements.txt",
			SystemPackages: []string{
				"libgl1-mesa-glx",
				"libglib2.0-0",
			},
		},
	}
	require.Equal(t, want, conf)
}

func TestFindProjectRootDirShouldFindParentDir(t *testing.T) {
	projectDir, err := ioutil.TempDir("", "cog-test")
	require.NoError(t, err)
	defer os.RemoveAll(projectDir)

	err = ioutil.WriteFile(path.Join(projectDir, "cog.yaml"), []byte(TEST_CONFIG), 0644)
	require.NoError(t, err)

	subdir := path.Join(projectDir, "some/sub/dir")
	err = os.MkdirAll(subdir, 0700)
	require.NoError(t, err)

	foundDir, err := findProjectRootDir(subdir)
	require.NoError(t, err)
	require.Equal(t, foundDir, projectDir)
}

func TestFindProjectRootDirShouldReturnErrIfNoConfig(t *testing.T) {
	projectDir, err := ioutil.TempDir("", "cog-test")
	require.NoError(t, err)
	defer os.RemoveAll(projectDir)

	subdir := path.Join(projectDir, "some/sub/dir")
	err = os.MkdirAll(subdir, 0700)
	require.NoError(t, err)

	_, err = findProjectRootDir(subdir)
	require.Error(t, err)
}
