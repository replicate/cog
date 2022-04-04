package dockerfile

import (
	// blank import for embeds
	_ "embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/config"
)

//go:embed embed/cog.whl
var cogWheelEmbed []byte

type Generator struct {
	Config *config.Config
	Dir    string

	// these are here to make this type testable
	GOOS   string
	GOARCH string

	// absolute path to tmpDir, a directory that will be cleaned up
	tmpDir string
	// tmpDir relative to Dir
	relativeTmpDir string
}

func NewGenerator(config *config.Config, dir string) (*Generator, error) {
	rootTmp := path.Join(dir, ".cog/tmp")
	if err := os.MkdirAll(rootTmp, 0o755); err != nil {
		return nil, err
	}
	// tmpDir ends up being something like dir/.cog/tmp/build123456789
	tmpDir, err := os.MkdirTemp(rootTmp, "build")
	if err != nil {
		return nil, err
	}
	// tmpDir, but without dir prefix. This is the path used in the Dockerfile.
	relativeTmpDir, err := filepath.Rel(dir, tmpDir)
	if err != nil {
		return nil, err
	}

	return &Generator{
		Config:         config,
		Dir:            dir,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOOS,
		tmpDir:         tmpDir,
		relativeTmpDir: relativeTmpDir,
	}, nil
}

func (g *Generator) GenerateBase() (string, error) {
	baseImage, err := g.baseImage()
	if err != nil {
		return "", err
	}
	installPython := ""
	if g.Config.Build.GPU {
		installPython, err = g.installPython()
		if err != nil {
			return "", err
		}
	}
	aptInstalls, err := g.aptInstalls()
	if err != nil {
		return "", err
	}
	pythonRequirements, err := g.pythonRequirements()
	if err != nil {
		return "", err

	}
	pipInstalls, err := g.pipInstalls()
	if err != nil {
		return "", err
	}
	installCog, err := g.installCog()
	if err != nil {
		return "", err
	}
	run, err := g.run()
	if err != nil {
		return "", err
	}

	return strings.Join(filterEmpty([]string{
		"# syntax = docker/dockerfile:1.2",
		"FROM " + baseImage,
		g.preamble(),
		installPython,
		installCog,
		aptInstalls,
		pythonRequirements,
		pipInstalls,
		run,
		`WORKDIR /src`,
		`EXPOSE 5000`,
		`CMD ["python", "-m", "cog.server.http"]`,
	}), "\n"), nil
}

func (g *Generator) Generate() (string, error) {
	base, err := g.GenerateBase()
	if err != nil {
		return "", err
	}
	return strings.Join(filterEmpty([]string{
		base,
		`COPY . /src`,
	}), "\n"), nil
}

func (g *Generator) Cleanup() error {
	if err := os.RemoveAll(g.tmpDir); err != nil {
		return fmt.Errorf("Failed to clean up %s: %w", g.tmpDir, err)
	}
	return nil
}

func (g *Generator) baseImage() (string, error) {
	if g.Config.Build.GPU {
		return g.Config.CUDABaseImageTag()
	}
	return "python:" + g.Config.Build.PythonVersion, nil
}

func (g *Generator) preamble() string {
	return `ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin`
}

func (g *Generator) aptInstalls() (string, error) {
	packages := g.Config.Build.SystemPackages
	if len(packages) == 0 {
		return "", nil
	}
	return "RUN --mount=type=cache,target=/var/cache/apt apt-get update -qq && apt-get install -qqy " +
		strings.Join(packages, " ") +
		" && rm -rf /var/lib/apt/lists/*", nil
}

func (g *Generator) installPython() (string, error) {
	// TODO: check that python version is valid

	py := g.Config.Build.PythonVersion

	return `ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
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
` + fmt.Sprintf(`RUN curl -s -S -L https://raw.githubusercontent.com/pyenv/pyenv-installer/master/bin/pyenv-installer | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s") && \
	pip install "wheel<1"`, py, py), nil
}

func (g *Generator) installCog() (string, error) {
	// Wheel name needs to be full format otherwise pip refuses to install it
	cogFilename := "cog-0.0.1.dev-py3-none-any.whl"
	cogPath := filepath.Join(g.tmpDir, cogFilename)
	if err := os.MkdirAll(filepath.Dir(cogPath), 0o755); err != nil {
		return "", fmt.Errorf("Failed to write %s: %w", cogFilename, err)
	}
	if err := os.WriteFile(cogPath, cogWheelEmbed, 0o644); err != nil {
		return "", fmt.Errorf("Failed to write %s: %w", cogFilename, err)
	}
	return fmt.Sprintf(`COPY %s /tmp/%s
RUN --mount=type=cache,target=/root/.cache/pip pip install /tmp/%s`, path.Join(g.relativeTmpDir, cogFilename), cogFilename, cogFilename), nil
}

func (g *Generator) pythonRequirements() (string, error) {
	reqs := g.Config.Build.PythonRequirements
	if reqs == "" {
		return "", nil
	}
	return fmt.Sprintf(`COPY %s /tmp/requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt`, reqs), nil
}

func (g *Generator) pipInstalls() (string, error) {
	packages, indexURLs, err := g.Config.PythonPackagesForArch(g.GOOS, g.GOARCH)
	if err != nil {
		return "", err
	}
	if len(packages) == 0 {
		return "", nil
	}

	findLinks := ""
	for _, indexURL := range indexURLs {
		findLinks += "-f " + indexURL + " "
	}
	for _, indexURL := range g.Config.Build.PythonFindLinks {
		findLinks += "-f " + indexURL + " "
	}
	extraIndexURLs := ""
	for _, indexURL := range g.Config.Build.PythonExtraIndexURLs {
		extraIndexURLs += "--extra-index-url=" + indexURL
	}

	return "RUN --mount=type=cache,target=/root/.cache/pip pip install " + findLinks + " " + extraIndexURLs + " " + strings.Join(packages, " "), nil
}

func (g *Generator) run() (string, error) {
	runCommands := g.Config.Build.Run

	// For backwards compatibility
	runCommands = append(runCommands, g.Config.Build.PreInstall...)

	lines := []string{}
	for _, run := range runCommands {
		run = strings.TrimSpace(run)
		if strings.Contains(run, "\n") {
			return "", fmt.Errorf(`One of the commands in 'run' contains a new line, which won't work. You need to create a new list item in YAML prefixed with '-' for each command.

This is the offending line: %s`, run)
		}
		lines = append(lines, "RUN "+run)
	}
	return strings.Join(lines, "\n"), nil
}

func filterEmpty(list []string) []string {
	filtered := []string{}
	for _, s := range list {
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
