package dockerfile

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func testInstallCog(generatedPaths []string) string {
	return `COPY .cog/tmp/cog-0.0.1.dev-py3-none-any.whl /tmp/cog-0.0.1.dev-py3-none-any.whl
RUN pip install /tmp/cog-0.0.1.dev-py3-none-any.whl`
}

func testInstallPython(version string) string {
	return fmt.Sprintf(`ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
RUN apt-get update -q && apt-get install -qy --no-install-recommends \
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
RUN curl https://pyenv.run | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s")
`, version, version)
}

func TestGenerateEmptyCPU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.ConfigFromYAML([]byte(`
model: predict.py:Model
environment:
  gpu: false
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen := DockerfileGenerator{Config: conf, Dir: tmpDir}
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `FROM python:3.8
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallCog(gen.generatedPaths) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateEmptyGPU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.ConfigFromYAML([]byte(`
model: predict.py:Model
environment:
  gpu: true
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())
	gen := DockerfileGenerator{Config: conf, Dir: tmpDir}
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `FROM nvidia/cuda:11.0-cudnn8-devel-ubuntu16.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallPython("3.8") + testInstallCog(gen.generatedPaths) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateFullCPU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.ConfigFromYAML([]byte(`
environment:
  gpu: false
  python_requirements: my-requirements.txt
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  system_packages:
    - ffmpeg
    - cowsay
model: predict.py:Model
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen := DockerfileGenerator{Config: conf, Dir: tmpDir}
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `FROM python:3.8
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallCog(gen.generatedPaths) + `
RUN apt-get update -qq && apt-get install -qy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
COPY my-requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN pip install -f https://download.pytorch.org/whl/torch_stable.html   torch==1.5.1+cpu pandas==1.2.0.12
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)
}

func TestGenerateFullGPU(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)

	conf, err := config.ConfigFromYAML([]byte(`
environment:
  gpu: true
  python_requirements: my-requirements.txt
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  system_packages:
    - ffmpeg
    - cowsay
model: predict.py:Model
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen := DockerfileGenerator{Config: conf, Dir: tmpDir}
	actual, err := gen.Generate()
	require.NoError(t, err)

	expected := `FROM nvidia/cuda:10.2-cudnn8-devel-ubuntu18.04
ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu
` + testInstallPython("3.8") +
		testInstallCog(gen.generatedPaths) + `
RUN apt-get update -qq && apt-get install -qy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
COPY my-requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN pip install   torch==1.5.1 pandas==1.2.0.12
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}
