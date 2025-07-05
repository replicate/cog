package dockerfile

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/dockertest"
	"github.com/replicate/cog/pkg/registry/registrytest"
)

func testTini() string {
	return `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked set -eux; \
apt-get update -qq && \
apt-get install -qqy --no-install-recommends curl; \
rm -rf /var/lib/apt/lists/*; \
TINI_VERSION=v0.19.0; \
TINI_ARCH="$(dpkg --print-architecture)"; \
curl -sSL -o /sbin/tini "https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-${TINI_ARCH}"; \
chmod +x /sbin/tini
ENTRYPOINT ["/sbin/tini", "--"]
`
}

func getWheelName() string {
	files, err := CogEmbed.ReadDir("embed")
	if err != nil {
		panic(err)
	}
	if len(files) != 1 {
		panic("couldn't find wheel embed or too many files in embed")
	}
	return files[0].Name()
}

func testInstallCog(relativeTmpDir string, stripped bool) string {
	wheel := getWheelName()
	strippedCall := ""
	if stripped {
		strippedCall += " && find / -type f -name \"*python*.so\" -not -name \"*cpython*.so\" -exec strip -S {} \\;"
	}
	return fmt.Sprintf(`COPY %s/%s /tmp/%s
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install --no-cache-dir /tmp/%s 'pydantic>=1.9,<3'%s
ENV CFLAGS=`, relativeTmpDir, wheel, wheel, wheel, strippedCall)
}

func testInstallPython(version string) string {
	return fmt.Sprintf(`ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy --no-install-recommends \
	make \
	build-essential \
	libssl-dev \
	zlib1g-dev \
	libbz2-dev \
	libreadline-dev \
	libsqlite3-dev \
	wget \
	curl \
	llvm \
	libncurses5-dev \
	libncursesw5-dev \
	xz-utils \
	tk-dev \
	libffi-dev \
	liblzma-dev \
	git \
	ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
RUN --mount=type=cache,target=/root/.cache/pip curl -s -S -L https://raw.githubusercontent.com/pyenv/pyenv-installer/master/bin/pyenv-installer | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	export PYTHON_CONFIGURE_OPTS='--enable-optimizations --with-lto' && \
	export PYTHON_CFLAGS='-O3' && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s") && \
	pip install "wheel<1"
`, version, version)
}

func TestGenerateEmptyCPU(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  python_version: "3.12"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + testInstallCog(gen.relativeTmpDir, gen.strip) + `
RUN find / -type f -name "*python*.so" -printf "%h\n" | sort -u > /etc/ld.so.conf.d/cog.conf && ldconfig
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateEmptyGPU(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  gpu: true
  python_version: "3.12"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + testInstallPython("3.12") + "RUN rm -rf /usr/bin/python3 && ln -s `realpath \\`pyenv which python\\`` /usr/bin/python3 && chmod +x /usr/bin/python3" + `
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
RUN find / -type f -name "*python*.so" -printf "%h\n" | sort -u > /etc/ld.so.conf.d/cog.conf && ldconfig
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateFullCPU(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.3.0
    - pandas==1.2.0.12
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt
ENV CFLAGS=
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
RUN find / -type f -name "*python*.so" -printf "%h\n" | sort -u > /etc/ld.so.conf.d/cog.conf && ldconfig
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)

	require.Equal(t, `--extra-index-url https://download.pytorch.org/whl/cpu
torch==2.3.0
pandas==1.2.0.12`, string(requirements))
}

func TestGenerateFullGPU(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  gpu: true
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.0.1
    - pandas==2.0.3
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
` + testInstallPython("3.12") + "RUN rm -rf /usr/bin/python3 && ln -s `realpath \\`pyenv which python\\`` /usr/bin/python3 && chmod +x /usr/bin/python3" + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt
ENV CFLAGS=
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
RUN find / -type f -name "*python*.so" -printf "%h\n" | sort -u > /etc/ld.so.conf.d/cog.conf && ldconfig
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)
	require.Equal(t, `--extra-index-url https://download.pytorch.org/whl/cu118
torch==2.0.1
pandas==2.0.3`, string(requirements))
}

// pre_install is deprecated but supported for backwards compatibility
func TestPreInstall(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  python_version: "3.12"
  system_packages:
    - cowsay
  pre_install:
    - "cowsay moo"
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
RUN find / -type f -name "*python*.so" -printf "%h\n" | sort -u > /etc/ld.so.conf.d/cog.conf && ldconfig
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)

}

func TestPythonRequirements(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(path.Join(tmpDir, "my-requirements.txt"), []byte("torch==1.0.0"), 0o644)
	require.NoError(t, err)
	conf, err := config.FromYAML([]byte(`
build:
  python_version: "3.12"
  python_requirements: "my-requirements.txt"
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(tmpDir))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)
	fmt.Println(actual)
	require.Contains(t, actual, `pip install -r /tmp/requirements.txt`)
}

// mockFileInfo is a test type to mock os.FileInfo
type mockFileInfo struct {
	size int64
}

func (mfi mockFileInfo) Size() int64 {
	return mfi.size
}
func (mfi mockFileInfo) Name() string {
	return ""
}
func (mfi mockFileInfo) Mode() os.FileMode {
	return 0
}
func (mfi mockFileInfo) ModTime() time.Time {
	return time.Time{}
}
func (mfi mockFileInfo) IsDir() bool {
	return false
}
func (mfi mockFileInfo) Sys() interface{} {
	return nil
}

const sizeThreshold = 10 * 1024 * 1024

func TestGenerateWithLargeModels(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  gpu: true
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.0.1
    - pandas==2.0.3
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)

	gen.fileWalker = func(root string, walkFn filepath.WalkFunc) error {
		for _, path := range []string{"checkpoints/large-a", "models/large-b", "root-large"} {
			walkFn(path, mockFileInfo{size: sizeThreshold}, nil)
		}
		return nil
	}

	modelDockerfile, runnerDockerfile, dockerignore, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM scratch

COPY checkpoints /src/checkpoints
COPY models /src/models
COPY root-large /src/root-large`

	require.Equal(t, expected, modelDockerfile)

	// model copy should be run before dependency install and code copy
	expected = `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*` + `
` + testInstallPython("3.12") + `RUN rm -rf /usr/bin/python3 && ln -s ` + "`realpath \\`pyenv which python\\`` /usr/bin/python3 && chmod +x /usr/bin/python3" + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt
ENV CFLAGS=
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
RUN find / -type f -name "*python*.so" -printf "%h\n" | sort -u > /etc/ld.so.conf.d/cog.conf && ldconfig
RUN cowsay moo
COPY --from=weights --link /src/checkpoints /src/checkpoints
COPY --from=weights --link /src/models /src/models
COPY --from=weights --link /src/root-large /src/root-large
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, runnerDockerfile)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)
	require.Equal(t, `--extra-index-url https://download.pytorch.org/whl/cu118
torch==2.0.1
pandas==2.0.3`, string(requirements))

	expected = `# generated by replicate/cog
__pycache__
*.pyc
*.pyo
*.pyd
.Python
env
pip-log.txt
pip-delete-this-directory.txt
.tox
.coverage
.coverage.*
.cache
nosetests.xml
coverage.xml
*.cover
*.log
.git
.mypy_cache
.pytest_cache
.hypothesis
checkpoints
checkpoints/**/*
models
models/**/*
root-large
`
	require.Equal(t, expected, dockerignore)
}

func TestGenerateDockerfileWithoutSeparateWeights(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  python_version: "3.12"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(false)
	actual, err := gen.GenerateDockerfileWithoutSeparateWeights(t.Context())
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + testInstallCog(gen.relativeTmpDir, gen.strip) + `
RUN find / -type f -name "*python*.so" -printf "%h\n" | sort -u > /etc/ld.so.conf.d/cog.conf && ldconfig
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateEmptyCPUWithCogBaseImage(t *testing.T) {
	tmpDir := t.TempDir()
	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  python_version: "3.12"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName("", "3.12", ""))
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:python3.12
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGeneratePythonCPUWithCogBaseImage(t *testing.T) {
	tmpDir := t.TempDir()

	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - pandas==1.2.0.12
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName("", "3.12", ""))
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:python3.12
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt
ENV CFLAGS=
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)
	require.Equal(t, `pandas==1.2.0.12`, string(requirements))
}

func TestGenerateFullGPUWithCogBaseImage(t *testing.T) {
	tmpDir := t.TempDir()
	client := registrytest.NewMockRegistryClient()
	command := dockertest.NewMockCommand()
	torchVersions := []string{"2.3", "2.3.0", "2.3.1"}
	for _, torchVersion := range torchVersions {
		yaml := fmt.Sprintf(`
build:
  gpu: true
  cuda: "11.8"
  python_version: "3.11"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==%s
    - pandas==2.0.3
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`, torchVersion)
		conf, err := config.FromYAML([]byte(yaml))
		require.NoError(t, err)
		require.NoError(t, conf.ValidateAndComplete(""))
		client.AddMockImage(BaseImageName("11.8", "3.11", torchVersion))
		gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
		require.NoError(t, err)
		gen.SetUseCogBaseImage(true)
		_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
		require.NoError(t, err)

		// We add the patch version to the expected torch version
		expectedTorchVersion := torchVersion
		if torchVersion == "2.3" {
			expectedTorchVersion = "2.3.1"
		}
		expected := fmt.Sprintf(`#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:cuda11.8-python3.11-torch%s
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
`+testInstallCog(gen.relativeTmpDir, gen.strip)+`
COPY `+gen.relativeTmpDir+`/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt
ENV CFLAGS=
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`, expectedTorchVersion)

		require.Equal(t, expected, actual)

		requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
		require.NoError(t, err)
		expected = fmt.Sprintf(`--extra-index-url https://download.pytorch.org/whl/cu118
torch==%s
pandas==2.0.3`, expectedTorchVersion)
		require.Equal(t, expected, string(requirements))
	}
}

func TestGenerateTorchWithStrippedModifiedVersion(t *testing.T) {
	tmpDir := t.TempDir()

	yaml := `
build:
  gpu: true
  cuda: "11.8"
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.3.1+cu118
    - pandas==2.0.3
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`
	conf, err := config.FromYAML([]byte(yaml))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName("11.8", "3.12", "2.3.1"))
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:cuda11.8-python3.12-torch2.3.1
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt
ENV CFLAGS=
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)
	require.Equal(t, `--extra-index-url https://download.pytorch.org/whl/cu118
torch==2.3.1
pandas==2.0.3`, string(requirements))
}

func TestGenerateWithStrip(t *testing.T) {
	tmpDir := t.TempDir()

	yaml := `
build:
  gpu: true
  cuda: "11.8"
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.3.1
    - pandas==2.0.3
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`
	conf, err := config.FromYAML([]byte(yaml))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName("11.8", "3.12", "2.3.1"))
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	gen.SetStrip(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:cuda11.8-python3.12-torch2.3.1
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt && find / -type f -name "*python*.so" -not -name "*cpython*.so" -exec strip -S {} \;
ENV CFLAGS=
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)
	require.Equal(t, `--extra-index-url https://download.pytorch.org/whl/cu118
torch==2.3.1
pandas==2.0.3`, string(requirements))
}

func TestGenerateDoesNotContainDangerousCFlags(t *testing.T) {
	tmpDir := t.TempDir()

	yaml := `
build:
  gpu: true
  cuda: "11.8"
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.3.1
    - pandas==2.0.3
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`
	conf, err := config.FromYAML([]byte(yaml))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName("11.8", "3.12", "2.3.1"))
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	require.NotContains(t, actual, "-march=native")
	require.NotContains(t, actual, "-mtune=native")
}

func TestGenerateWithPrecompile(t *testing.T) {
	tmpDir := t.TempDir()

	yaml := `
build:
  gpu: true
  cuda: "11.8"
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.3.1
    - pandas==2.0.3
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`
	conf, err := config.FromYAML([]byte(yaml))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName("11.8", "3.12", "2.3.1"))
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	gen.SetStrip(true)
	gen.SetPrecompile(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:cuda11.8-python3.12-torch2.3.1
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
` + testInstallCog(gen.relativeTmpDir, gen.strip) + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt && find / -type f -name "*python*.so" -not -name "*cpython*.so" -exec strip -S {} \;
ENV CFLAGS=
RUN find / -type f -name "*.py[co]" -delete && find / -type f -name "*.py" -exec touch -t 197001010000 {} \; && find / -type f -name "*.py" -printf "%h\n" | sort -u | /usr/bin/python3 -m compileall --invalidation-mode timestamp -o 2 -j 0
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)
	require.Equal(t, `--extra-index-url https://download.pytorch.org/whl/cu118
torch==2.3.1
pandas==2.0.3`, string(requirements))
}

func TestGenerateWithCoglet(t *testing.T) {
	tmpDir := t.TempDir()

	yaml := `
build:
  gpu: true
  cuda: "11.8"
  python_version: "3.12"
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==2.3.1
    - pandas==2.0.3
    - coglet @ https://github.com/replicate/cog-runtime/releases/download/v0.1.0-alpha31/coglet-0.1.0a31-py3-none-any.whl
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`
	conf, err := config.FromYAML([]byte(yaml))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	command := dockertest.NewMockCommand()
	client := registrytest.NewMockRegistryClient()
	client.AddMockImage(BaseImageName("11.8", "3.12", "2.3.1"))
	gen, err := NewStandardGenerator(conf, tmpDir, command, client, true)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	gen.SetStrip(true)
	gen.SetPrecompile(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights(t.Context(), "r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:cuda11.8-python3.12-torch2.3.1
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
ENV CFLAGS="-O3 -funroll-loops -fno-strict-aliasing -flto -S"
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt && find / -type f -name "*python*.so" -not -name "*cpython*.so" -exec strip -S {} \;
ENV CFLAGS=
RUN find / -type f -name "*.py[co]" -delete && find / -type f -name "*.py" -exec touch -t 197001010000 {} \; && find / -type f -name "*.py" -printf "%h\n" | sort -u | /usr/bin/python3 -m compileall --invalidation-mode timestamp -o 2 -j 0
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)

	requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
	require.NoError(t, err)
	require.Equal(t, `--extra-index-url https://download.pytorch.org/whl/cu118
torch==2.3.1
pandas==2.0.3
coglet @ https://github.com/replicate/cog-runtime/releases/download/v0.1.0-alpha31/coglet-0.1.0a31-py3-none-any.whl`, string(requirements))
}
