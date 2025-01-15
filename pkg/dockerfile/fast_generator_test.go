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
	require.Equal(t, "# syntax=docker/dockerfile:1-labs", dockerfileLines[0])
}

func TestGenerateUVCacheMount(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{
			"torch==2.5.1",
			"catboost==1.2.7",
		},
	}
	config := config.Config{
		Build: &build,
	}
	generator, err := NewFastGenerator(&config, dir)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=type=bind,ro,source=\".cog/tmp\",target=\"/buildtmp\" --mount=type=cache,from=usercache,target=\"/var/cache/monobase\" --mount=type=cache,target=/var/cache/apt,id=apt-cache --mount=type=cache,target=/srv/r8/monobase/uv/cache,id=pip-cache UV_CACHE_DIR=\"/srv/r8/monobase/uv/cache\" UV_LINK_MODE=copy /opt/r8/monobase/run.sh monobase.build --mini --cache=/var/cache/monobase", dockerfileLines[4])
}

func TestGenerateCUDA(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		GPU:  true,
		CUDA: "12.4",
	}
	config := config.Config{
		Build: &build,
	}
	generator, err := NewFastGenerator(&config, dir)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "ENV R8_CUDA_VERSION=12.4", dockerfileLines[3])
}

func TestGeneratePythonPackages(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonPackages: []string{
			"catboost==1.2.7",
		},
	}
	config := config.Config{
		Build: &build,
	}
	generator, err := NewFastGenerator(&config, dir)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=type=bind,ro,source=\".cog/tmp\",target=\"/buildtmp\" --mount=type=cache,from=usercache,target=\"/var/cache/monobase\" --mount=type=cache,target=/srv/r8/monobase/uv/cache,id=pip-cache UV_CACHE_DIR=\"/srv/r8/monobase/uv/cache\" UV_LINK_MODE=copy /opt/r8/monobase/run.sh monobase.build --requirements=/buildtmp/requirements.txt --cache=/var/cache/monobase", dockerfileLines[5])
}
