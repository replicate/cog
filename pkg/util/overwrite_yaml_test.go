package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

/*
func TestOverwriteYAML(t *testing.T) {
	var yamlData1 = `build:
    command: "build.sh"
image: "my-image"
predict: "predict.py"
train: "train.py"
concurrency:
    max: 5
environment:
    - "VAR1=value1"
    - "VAR2=value2"
`

	var yamlData2 = `build:
  command: "build_new.sh"
image: "new-image"
predict: "new_predict.py"
concurrency:
  max: 10
environment:
  - "VAR1=new_value1"
  - "VAR3=value3"
`
	content, err := OverwriteYAML([]byte(yamlData1), []byte(yamlData2))
	require.NoError(t, err)
	require.Equal(t, yamlData1, string(content))
}
*/

func TestOverwriteYAMLWithComments(t *testing.T) {
	var sourceYaml = `build:
  command: "build_new.sh"
image: "new-image"
predict: "new_predict.py"
concurrency:
  max: 10
environment:
  - "VAR1=new_value1"
  - "VAR3=value3"
`

	var destinationYaml = `# This here is a YAML Comment
build:
    command: "build.sh"
image: "my-image"
predict: "predict.py"
train: "train.py"
concurrency:
    max: 5
environment:
    - "VAR1=value1"
    - "VAR2=value2"
`

	expected := `# This here is a YAML Comment
build:
    command: "build_new.sh"
image: "new-image"
predict: "new_predict.py"
concurrency:
    max: 10
environment:
    - "VAR1=new_value1"
    - "VAR3=value3"
`

	content, err := OverwriteYAML([]byte(sourceYaml), []byte(destinationYaml))
	require.NoError(t, err)
	require.Equal(t, expected, string(content))
}

func TestOverwriteYAMLWithLineComments(t *testing.T) {
	var sourceYaml = `build:
  command: "build_new.sh"
image: "new-image"
predict: "new_predict.py"
concurrency:
  max: 10
environment:
  - "VAR1=new_value1"
  - "VAR3=value3"
`

	var destinationYaml = `# This here is a YAML Comment
build:
    # And we put this comment here for good measure
    command: "build.sh"
image: "my-image"
predict: "predict.py"
train: "train.py"
concurrency:
    max: 5
environment:
    - "VAR1=value1"
    - "VAR2=value2"
`

	expected := `# This here is a YAML Comment
build:
    # And we put this comment here for good measure
    command: "build_new.sh"
image: "new-image"
predict: "new_predict.py"
concurrency:
    max: 10
environment:
    - "VAR1=new_value1"
    - "VAR3=value3"
`
	content, err := OverwriteYAML([]byte(sourceYaml), []byte(destinationYaml))
	require.NoError(t, err)
	require.Equal(t, expected, string(content))
}

func TestStep1XYaml(t *testing.T) {
	var sourceYaml = `build:
  gpu: true
  system_packages:
    - "libgl1-mesa-glx"
    - "libglib2.0-0"
  python_version: "3.11"
  python_requirements: requirements.txt
  fast: true
predict: "predict.py:Predictor"
`

	var destinationYaml = `# Configuration for Cog ⚙️
# Reference: https://cog.run/yaml

build:
  # set to true if your model requires a GPU
  gpu: true

  # a list of ubuntu apt packages to install
  system_packages:
    - "libgl1-mesa-glx"
    - "libglib2.0-0"

  # python version in the form '3.11' or '3.11.4'
  python_version: "3.11"

  # path to a Python requirements.txt file
  python_requirements: requirements.txt

  # commands run after the environment is setup
  run:
  - curl -o /usr/local/bin/pget -L "https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)"
  - chmod +x /usr/local/bin/pget

# predict.py defines how predictions are run on your model
predict: "predict.py:Predictor"`

	expected := `# Configuration for Cog ⚙️
# Reference: https://cog.run/yaml

build:
    # set to true if your model requires a GPU
    gpu: true
    # a list of ubuntu apt packages to install
    system_packages:
        - "libgl1-mesa-glx"
        - "libglib2.0-0"
    # python version in the form '3.11' or '3.11.4'
    python_version: "3.11"
    # path to a Python requirements.txt file
    python_requirements: requirements.txt
    fast: true
# predict.py defines how predictions are run on your model
predict: "predict.py:Predictor"
`
	content, err := OverwriteYAML([]byte(sourceYaml), []byte(destinationYaml))
	require.NoError(t, err)
	require.Equal(t, expected, string(content))
}
