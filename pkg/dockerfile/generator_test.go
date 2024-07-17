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

func testInstallCog(relativeTmpDir string) string {
	return fmt.Sprintf(`COPY %s/cog-0.0.1.dev-py3-none-any.whl /tmp/cog-0.0.1.dev-py3-none-any.whl
RUN --mount=type=cache,target=/root/.cache/pip pip install -t /dep /tmp/cog-0.0.1.dev-py3-none-any.whl`, relativeTmpDir)
}

func testPipInstallStage(relativeTmpDir string) string {
	return `FROM python:3.12 as deps
` + testInstallCog(relativeTmpDir)
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
RUN curl -s -S -L https://raw.githubusercontent.com/pyenv/pyenv-installer/master/bin/pyenv-installer | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
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
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
` + testPipInstallStage(gen.relativeTmpDir) + `
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `COPY --from=deps --link /dep /usr/local/lib/python3.12/site-packages
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
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))
	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
` + testPipInstallStage(gen.relativeTmpDir) + `
FROM nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + testInstallPython("3.12") + `RUN --mount=type=bind,from=deps,source=/dep,target=/dep \
    cp -rf /dep/* $(pyenv prefix)/lib/python*/site-packages; \
    cp -rf /dep/bin/* $(pyenv prefix)/bin; \
    pyenv rehash
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

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
` + testPipInstallStage(gen.relativeTmpDir) + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -t /dep -r /tmp/requirements.txt
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
COPY --from=deps --link /dep /usr/local/lib/python3.12/site-packages
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

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
` + testPipInstallStage(gen.relativeTmpDir) + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -t /dep -r /tmp/requirements.txt
FROM nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() +
		testInstallPython("3.12") + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN --mount=type=bind,from=deps,source=/dep,target=/dep \
    cp -rf /dep/* $(pyenv prefix)/lib/python*/site-packages; \
    cp -rf /dep/bin/* $(pyenv prefix)/bin; \
    pyenv rehash
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
  system_packages:
    - cowsay
  pre_install:
    - "cowsay moo"
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
` + testPipInstallStage(gen.relativeTmpDir) + `
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
COPY --from=deps --link /dep /usr/local/lib/python3.12/site-packages
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
  python_requirements: "my-requirements.txt"
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(tmpDir))

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)
	fmt.Println(actual)
	require.Contains(t, actual, `pip install -t /dep -r /tmp/requirements.txt`)
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

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)

	gen.fileWalker = func(root string, walkFn filepath.WalkFunc) error {
		for _, path := range []string{"checkpoints/large-a", "models/large-b", "root-large"} {
			walkFn(path, mockFileInfo{size: sizeThreshold}, nil)
		}
		return nil
	}

	modelDockerfile, runnerDockerfile, dockerignore, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
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
` + testPipInstallStage(gen.relativeTmpDir) + `
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -t /dep -r /tmp/requirements.txt
FROM nvidia/cuda:11.8.0-cudnn8-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() +
		testInstallPython("3.12") + `RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN --mount=type=bind,from=deps,source=/dep,target=/dep \
    cp -rf /dep/* $(pyenv prefix)/lib/python*/site-packages; \
    cp -rf /dep/bin/* $(pyenv prefix)/bin; \
    pyenv rehash
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
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.GenerateDockerfileWithoutSeparateWeights()
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
` + testPipInstallStage(gen.relativeTmpDir) + `
FROM python:3.12-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
ENV NVIDIA_DRIVER_CAPABILITIES=all
` + testTini() + `COPY --from=deps --link /dep /usr/local/lib/python3.12/site-packages
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
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:python3.12
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
  system_packages:
    - ffmpeg
    - cowsay
  python_version: "3.12"
  python_packages:
    - pandas==1.2.0.12
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndComplete(""))

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	gen.SetUseCogBaseImage(true)
	_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
	require.NoError(t, err)

	expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:python3.12
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt
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

	torchVersions := []string{"2.3", "2.3.0", "2.3.1"}
	for _, torchVersion := range torchVersions {
		yaml := fmt.Sprintf(`
build:
  gpu: true
  cuda: "11.8"
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

		gen, err := NewGenerator(conf, tmpDir)
		require.NoError(t, err)
		gen.SetUseCogBaseImage(true)
		_, actual, _, err := gen.GenerateModelBaseWithSeparateWeights("r8.im/replicate/cog-test")
		require.NoError(t, err)

		expected := `#syntax=docker/dockerfile:1.4
FROM r8.im/replicate/cog-test-weights AS weights
FROM r8.im/cog-base:cuda11.8-python3.12-torch2.3
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
COPY ` + gen.relativeTmpDir + `/requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

		require.Equal(t, expected, actual)

		requirements, err := os.ReadFile(path.Join(gen.tmpDir, "requirements.txt"))
		require.NoError(t, err)
		require.Equal(t, "pandas==2.0.3", string(requirements))
	}
}
