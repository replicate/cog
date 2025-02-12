package dockerfile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:  "3.8",
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
		Tests: []config.Test{
			{
				Command: "cog predict -i s=world",
			},
		},
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "# syntax=docker/dockerfile:1-labs", dockerfileLines[0])
}

func TestGenerateUVCacheMount(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion: "3.8",
		PythonPackages: []string{
			"torch==2.5.1",
			"catboost==1.2.7",
		},
	}
	config := config.Config{
		Build: &build,
		Tests: []config.Test{
			{
				Command: "cog predict -i s=world",
			},
		},
	}
	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	command := dockertest.NewMockCommand()
	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=type=bind,ro,source=\".cog/tmp\",target=\"/buildtmp\" --mount=type=cache,from=usercache,target=\"/var/cache/monobase\" --mount=type=cache,target=/var/cache/apt,id=apt-cache,sharing=locked --mount=type=cache,target=/srv/r8/monobase/uv/cache,id=pip-cache UV_CACHE_DIR=\"/srv/r8/monobase/uv/cache\" UV_LINK_MODE=copy /opt/r8/monobase/run.sh monobase.build --mini --cache=/var/cache/monobase", dockerfileLines[4])
}

func TestGenerateCUDA(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		GPU:           true,
		CUDA:          "12.4",
		PythonVersion: "3.8",
	}
	config := config.Config{
		Build: &build,
		Tests: []config.Test{
			{
				Command: "cog predict -i s=world",
			},
		},
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "ENV R8_CUDA_VERSION=12.4", dockerfileLines[3])
}

func TestGeneratePythonPackages(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion: "3.8",
		PythonPackages: []string{
			"catboost==1.2.7",
		},
	}
	config := config.Config{
		Build: &build,
		Tests: []config.Test{
			{
				Command: "cog predict -i s=world",
			},
		},
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=type=bind,ro,source=\".cog/tmp\",target=\"/buildtmp\" --mount=type=cache,target=/srv/r8/monobase/uv/cache,id=pip-cache UV_CACHE_DIR=\"/srv/r8/monobase/uv/cache\" UV_LINK_MODE=copy UV_COMPILE_BYTECODE=0 /opt/r8/monobase/run.sh monobase.user --requirements=/buildtmp/requirements.txt", dockerfileLines[5])
}

func TestGenerateVerboseEnv(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:  "3.8",
		PythonPackages: []string{"torch==2.5.1"},
	}
	config := config.Config{
		Build: &build,
		Tests: []config.Test{
			{
				Command: "cog predict -i s=world",
			},
		},
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "ENV VERBOSE=0", dockerfileLines[8])
}

func TestAptInstall(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:  "3.8",
		SystemPackages: []string{"git"},
	}
	config := config.Config{
		Build: &build,
		Tests: []config.Test{
			{
				Command: "cog predict -i s=world",
			},
		},
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=type=bind,ro,source=\".cog/tmp\",target=\"/buildtmp\" tar -xf \"/buildtmp/apt.9a881b9b9f23849475296a8cd768ea1965bc3152df7118e60c145975af6aa58a.tar.zst\" -C /", dockerfileLines[5])
}

func TestValidateConfigWithBuildRunItems(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:  "3.8",
		SystemPackages: []string{"git"},
		Run: []config.RunItem{
			{
				Command: "echo \"I'm alive\"",
			},
		},
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}
	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)

	err = generator.validateConfig()
	require.Error(t, err)
}

func TestValidateConfigWithNoTests(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:  "3.8",
		SystemPackages: []string{"git"},
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.8"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.8",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}
	generator, err := NewFastGenerator(&config, dir, command, &matrix)
	require.NoError(t, err)

	err = generator.validateConfig()
	require.Error(t, err)
}
