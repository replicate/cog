package migrate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOverwrightYAML(t *testing.T) {
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
	content, err := OverwrightYAML([]byte(yamlData1), []byte(yamlData2))
	require.NoError(t, err)
	require.Equal(t, yamlData1, string(content))
}

func TestOverwrightYAMLWithComments(t *testing.T) {
	var yamlData1 = `# This here is a YAML Comment
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
	content, err := OverwrightYAML([]byte(yamlData1), []byte(yamlData2))
	require.NoError(t, err)
	require.Equal(t, yamlData1, string(content))
}
