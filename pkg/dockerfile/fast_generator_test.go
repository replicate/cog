package dockerfile

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/util/console"
)

func writeRequirements(t *testing.T, req string) string {
	srcDir := t.TempDir()
	reqFile := path.Join(srcDir, "requirements.txt")
	err := os.WriteFile(reqFile, []byte(req), 0o644)
	require.NoError(t, err)
	return reqFile
}

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:      "3.9",
		PythonRequirements: writeRequirements(t, "torch==2.5.1"),
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.9"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.9",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "# syntax=docker/dockerfile:1-labs", dockerfileLines[0])
}

func TestGenerateUVCacheMount(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:      "3.9",
		PythonRequirements: writeRequirements(t, "torch==2.5.1\ncatboost==1.2.7"),
	}
	config := config.Config{
		Build: &build,
	}
	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.9"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.9",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	command := dockertest.NewMockCommand()
	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=from=monobase,target=/buildtmp --mount=type=cache,target=/var/cache/monobase,id=monobase-cache --mount=type=cache,target=/srv/r8/monobase/uv/cache,id=uv-cache UV_CACHE_DIR=\"/srv/r8/monobase/uv/cache\" UV_LINK_MODE=copy /opt/r8/monobase/run.sh monobase.build --mini --cache=/var/cache/monobase", dockerfileLines[4])
}

func TestGenerateCUDA(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		GPU:           true,
		CUDA:          "12.4",
		PythonVersion: "3.9",
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"12.4"},
		CudnnVersions:  []string{"1"},
		PythonVersions: []string{"3.9"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.9",
				Torch:  "2.5.1",
				Cuda:   "12.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "ENV R8_CUDA_VERSION=12.4", dockerfileLines[3])
}

func TestGeneratePythonPackages(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:      "3.9",
		PythonRequirements: writeRequirements(t, "catboost==1.2.7"),
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.9"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.9",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=from=requirements,target=/buildtmp --mount=type=bind,src=\".\",target=/src,rw --mount=type=cache,target=/srv/r8/monobase/uv/cache,id=uv-cache cd /src && UV_CACHE_DIR=\"/srv/r8/monobase/uv/cache\" UV_LINK_MODE=copy UV_COMPILE_BYTECODE=0 /opt/r8/monobase/run.sh monobase.user --requirements=/buildtmp/requirements.txt", dockerfileLines[5])
}

func TestGenerateVerboseEnv(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:      "3.9",
		PythonRequirements: writeRequirements(t, "torch==2.5.1"),
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.9"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.9",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "ENV VERBOSE=0", dockerfileLines[8])
}

func TestAptInstall(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:  "3.9",
		SystemPackages: []string{"git"},
	}
	config := config.Config{
		Build: &build,
	}
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"2.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.9"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.9",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "RUN --mount=from=apt,target=/buildtmp tar --keep-directory-symlink -xf \"/buildtmp/apt.9a881b9b9f23849475296a8cd768ea1965bc3152df7118e60c145975af6aa58a.tar.zst\" -C /", dockerfileLines[5])
}

func TestValidateConfigWithBuildRunItems(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:  "3.9",
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
		PythonVersions: []string{"3.9"},
		TorchVersions:  []string{"2.5.1"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.9",
				Torch:  "2.5.1",
				Cuda:   "2.4",
			},
		},
	}
	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)

	err = generator.validateConfig()
	require.Error(t, err)
}

func TestTorchVersionDefaultCUDA(t *testing.T) {
	dir := t.TempDir()
	build := config.Build{
		PythonVersion:      "3.10",
		PythonRequirements: writeRequirements(t, "torch==2.5.0"),
		GPU:                true,
	}
	config := config.Config{
		Build: &build,
	}
	err := config.ValidateAndComplete(dir)
	require.NoError(t, err)
	command := dockertest.NewMockCommand()

	// Create matrix
	matrix := MonobaseMatrix{
		Id:             1,
		CudaVersions:   []string{"12.4"},
		CudnnVersions:  []string{"1.0"},
		PythonVersions: []string{"3.10"},
		TorchVersions:  []string{"2.5.0"},
		Venvs: []MonobaseVenv{
			{
				Python: "3.10",
				Torch:  "2.5.0",
				Cuda:   "12.4",
			},
		},
		TorchCUDAs: map[string][]string{
			"2.5.0": {"12.4"},
		},
	}

	generator, err := NewFastGenerator(&config, dir, command, &matrix, true)
	require.NoError(t, err)
	dockerfile, err := generator.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)
	console.Info(dockerfile)
	dockerfileLines := strings.Split(dockerfile, "\n")
	require.Equal(t, "ENV R8_CUDA_VERSION=12.4", dockerfileLines[4])
}
