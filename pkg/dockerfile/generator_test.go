package dockerfile

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func testInstallCog(relativeTmpDir string) string {
	return fmt.Sprintf(`COPY %s/cog-0.0.1.dev-py3-none-any.whl /tmp/cog-0.0.1.dev-py3-none-any.whl
RUN --mount=type=cache,target=/root/.cache/pip pip install /tmp/cog-0.0.1.dev-py3-none-any.whl`, relativeTmpDir)
}

func testInstallPython(version string) string {
	return fmt.Sprintf(`ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy --no-install-recommends \
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
	python-openssl \
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
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
` + testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateEmptyGPU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.FromYAML([]byte(`
build:
  gpu: true
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())
	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `# syntax = docker/dockerfile:1.2
FROM nvidia/cuda:11.2.0-cudnn8-devel-ubuntu20.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
` + testInstallPython("3.8") + testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateFullCPU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
` + testInstallCog(gen.relativeTmpDir) + `
RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN --mount=type=cache,target=/root/.cache/pip pip install -f https://download.pytorch.org/whl/torch_stable.html   torch==1.5.1+cpu pandas==1.2.0.12
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)
}

func TestGenerateFullGPU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.FromYAML([]byte(`
build:
  gpu: true
  system_packages:
    - ffmpeg
    - cowsay
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `# syntax = docker/dockerfile:1.2
FROM nvidia/cuda:10.2-cudnn8-devel-ubuntu18.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
` + testInstallPython("3.8") +
		testInstallCog(gen.relativeTmpDir) + `
RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN --mount=type=cache,target=/root/.cache/pip pip install   torch==1.5.1 pandas==1.2.0.12
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

// pre_install is deprecated but supported for backwards compatibility
func TestPreInstall(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.FromYAML([]byte(`
build:
  system_packages:
    - cowsay
  pre_install:
    - "cowsay moo"
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
` + testInstallCog(gen.relativeTmpDir) + `
RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
RUN cowsay moo
WORKDIR /src
EXPOSE 5000
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)

}

func TestPythonRequirements(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)
	conf, err := config.FromYAML([]byte(`
build:
  python_requirements: "my-requirements.txt"
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.Generate()
	require.NoError(t, err)
	require.Contains(t, actual, `COPY my-requirements.txt /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt`)
}
