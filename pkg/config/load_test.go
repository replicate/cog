package config

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/replicate/cog/pkg/model"
	"github.com/stretchr/testify/require"
)

const TEST_CONFIG = `model: "model.py:SomeModel"
environment:
  architectures:
    - cpu
  python_version: "3.8"
  python_requirements: requirements.txt
  system_packages:
    - libgl1-mesa-glx
    - libglib2.0-0
examples:
  - input:
      input: "@example/foo.jpg"
  - input:
      input: "@example/bar.jpg"
`

func TestGetProjectDirWithFlagSet(t *testing.T) {
	projectDirFlag := "foo"

	projectDir, err := GetProjectDir(projectDirFlag)
	require.NoError(t, err)
	require.Equal(t, projectDir, projectDirFlag)
}

func TestGetConfigShouldLoadFromFile(t *testing.T) {
	dir, err := ioutil.TempDir("/tmp/", "cog-test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	err = ioutil.WriteFile(path.Join(dir, "cog.yaml"), []byte(TEST_CONFIG), 0644)
	require.NoError(t, err)
	t.Log(dir)
	conf, _, err := GetConfig(dir)
	require.NoError(t, err)
	want := &model.Config{
		Model: "model.py:SomeModel",
		Environment: &model.Environment{
			PythonVersion:      "3.8",
			PythonRequirements: "requirements.txt",
			Architectures: []string{
				"cpu",
			},
			SystemPackages: []string{
				"libgl1-mesa-glx",
				"libglib2.0-0",
			},
		},
		Examples: []*model.Example{
			{
				Input: map[string]string{
					"input": "@example/foo.jpg",
				},
			},
			{
				Input: map[string]string{
					"input": "@example/bar.jpg",
				},
			},
		},
	}
	require.Equal(t, want, conf)
}
