package dockerfile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
	}
	generator, err := NewFastGenerator(&config, dir)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, dockerfileLines[0], "# syntax=docker/dockerfile:1-labs")
}
