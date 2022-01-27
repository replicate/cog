package dockerfile

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

//===============================================================================
// helpers
//-------------------------------------------------------------------------------

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
RUN curl https://pyenv.run | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s") && \
	pip install "wheel<1"
`, version, version)
}

func testPreamble() string {
	// Get default environment variables: the hardcoded parts. (Excluding override-able parts.)
	return `ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
`
}

func testPreambleDefault() string {
	// Get default environment variables INCLUDING the override-able parts (like cache home)
	return testPreamble() + `ENV XDG_CACHE_HOME=/src/.cache
`
}

func testGenerate(t *testing.T, conf *config.Config) (*Generator, string) {
	tmpDir, err2 := os.MkdirTemp("", "test")
	require.NoError(t, err2)
	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	actual, err := gen.Generate()
	require.NoError(t, err)
	return gen, actual
}

//===============================================================================
// tests: build.gpu
//-------------------------------------------------------------------------------

func TestGenerateEmptyCPU(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
` + testPreambleDefault() +
		testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateEmptyGPU(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: true
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM nvidia/cuda:11.2.0-cudnn8-devel-ubuntu20.04
` + testPreambleDefault() +
		testInstallPython("3.8") +
		testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestGenerateFullCPU(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  system_packages:
    - ffmpeg
    - cowsay
  python_requirements: my-requirements.txt
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
` + testPreambleDefault() +
		testInstallCog(gen.relativeTmpDir) + `
RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
COPY my-requirements.txt /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -f https://download.pytorch.org/whl/torch_stable.html   torch==1.5.1+cpu pandas==1.2.0.12
RUN cowsay moo
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)
}

func TestGenerateFullGPU(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: true
  system_packages:
    - ffmpeg
    - cowsay
  python_requirements: my-requirements.txt
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  run:
    - "cowsay moo"
predict: predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM nvidia/cuda:10.2-cudnn8-devel-ubuntu18.04
` + testPreambleDefault() +
		testInstallPython("3.8") +
		testInstallCog(gen.relativeTmpDir) + `
RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
COPY my-requirements.txt /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install   torch==1.5.1 pandas==1.2.0.12
RUN cowsay moo
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

//===============================================================================
// tests: build.gpu
//-------------------------------------------------------------------------------

func TestBuildEnvironmentVariables(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  environment:
    - FOOBAR=foobar
    - XDG_CACHE_HOME=/src/custom_xdg_cache_home
predict: cog_predict.py:Predictor
`))
	expectPreamble := testPreamble() +
		`ENV FOOBAR=foobar
ENV XDG_CACHE_HOME=/src/custom_xdg_cache_home
`
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
` + expectPreamble +
		testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestBuildEmptyEnvironmentVariables(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  environment: []
predict: cog_predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
` + testPreambleDefault() +
		testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`

	require.Equal(t, expected, actual)
}

func TestBuildEnvironmentVariablesMultiple(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  environment:
    - EXAMPLE=example
    - XDG_CACHE_HOME=/src/precached
    - TORCH_HOME=$XDG_CACHE_HOME/torchy
    - "PYTORCH_TRANSFORMERS_CACHE=$TORCH_HOME/custom"
    - TRANSFORMERS_CACHE=$XDG_CACHE_HOME/huggingface
    - empty_is_ok=
predict: cog_predict.py:Predictor
`))
	expectedPreamble := testPreamble() +
		`ENV EXAMPLE=example
ENV XDG_CACHE_HOME=/src/precached
ENV TORCH_HOME=$XDG_CACHE_HOME/torchy
ENV PYTORCH_TRANSFORMERS_CACHE=$TORCH_HOME/custom
ENV TRANSFORMERS_CACHE=$XDG_CACHE_HOME/huggingface
ENV empty_is_ok=
`
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
` + expectedPreamble +
		testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)
}

func TestBuildEnvironmentInheritCacheHome(t *testing.T) {
	// Confirm that, even if XDG_CACHE_HOME is not provided,
	// it still resolves to the default /src/.cache (inside WORKDIR).

	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  environment:  # You don't have to override XDG_CACHE_HOME to "inherit" from it!
    - PYTORCH_PRETRAINED_BERT_CACHE=$XDG_CACHE_HOME/berty
predict: cog_predict.py:Predictor
`))
	expectedPreamble := testPreambleDefault() +
		`ENV PYTORCH_PRETRAINED_BERT_CACHE=$XDG_CACHE_HOME/berty
`
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
` + expectedPreamble +
		testInstallCog(gen.relativeTmpDir) + `
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)
}

func TestBuildInvalidEnvironmentVariables(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  gpu: false
  environment:
    - "0=invalid"
predict: cog_predict.py:Predictor
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	tmpDir, err := os.MkdirTemp("", "test")
	require.NoError(t, err)
	gen, err := NewGenerator(conf, tmpDir)
	require.NoError(t, err)
	_, err = gen.Generate()
	require.Error(t, err, fmt.Errorf("invalid environment variable: 0=invalid"))
}

// ================================================================
// tests: the rest
// ----------------------------------------------------------------

// pre_install is deprecated but supported for backwards compatibility
func TestPreInstall(t *testing.T) {
	conf, err := config.FromYAML([]byte(`
build:
  system_packages:
    - cowsay
  pre_install:
    - "cowsay moo"
`))
	require.NoError(t, err)
	require.NoError(t, conf.ValidateAndCompleteConfig())

	gen, actual := testGenerate(t, conf)

	expected := `# syntax = docker/dockerfile:1.2
FROM python:3.8
` + testPreambleDefault() +
		testInstallCog(gen.relativeTmpDir) + `
RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy cowsay && rm -rf /var/lib/apt/lists/*
RUN cowsay moo
WORKDIR /src
CMD ["python", "-m", "cog.server.http"]
COPY . /src`
	require.Equal(t, expected, actual)

}
