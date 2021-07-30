package dockerfile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/global"
)

//go:embed embed/cog.whl
var cogWheelEmbed []byte

type DockerfileGenerator struct {
	Config *config.Config
	Dir    string

	// these are here to make this type testable
	GOOS   string
	GOARCH string

	// to clean up
	generatedPaths []string
}

func NewGenerator(config *config.Config, dir string) *DockerfileGenerator {
	return &DockerfileGenerator{
		Config:         config,
		Dir:            dir,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOOS,
		generatedPaths: []string{},
	}
}

func (g *DockerfileGenerator) GenerateBase() (string, error) {
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

	configJson, err := json.Marshal(g.Config)
	if err != nil {
		return "", err
	}

	return strings.Join(filterEmpty([]string{
		"FROM " + baseImage,
		g.preamble(),
		installPython,
		installCog,
		aptInstalls,
		pythonRequirements,
		pipInstalls,
		g.preInstall(),
		g.workdir(),
		g.command(),
		// Add labels at end so cache isn't busted
		label(global.LabelNamespace+"cog_version", global.Version),
		label(global.LabelNamespace+"config", string(bytes.TrimSpace(configJson))),
	}), "\n"), nil
}

func (g *DockerfileGenerator) Generate() (string, error) {
	base, err := g.GenerateBase()
	if err != nil {
		return "", err
	}
	return strings.Join(filterEmpty([]string{
		base,
		g.copyCode(),
	}), "\n"), nil
}

func (g *DockerfileGenerator) Cleanup() error {
	for _, generatedPath := range g.generatedPaths {
		if err := os.Remove(generatedPath); err != nil {
			return fmt.Errorf("Failed to clean up %s: %w", generatedPath, err)
		}
	}
	return nil
}

func (g *DockerfileGenerator) baseImage() (string, error) {
	if g.Config.Build.GPU {
		return g.Config.CUDABaseImageTag()
	}
	return "python:" + g.Config.Build.PythonVersion, nil
}

func (g *DockerfileGenerator) preamble() string {
	// TODO: other stuff
	return `ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin`
}

func (g *DockerfileGenerator) aptInstalls() (string, error) {
	packages := g.Config.Build.SystemPackages
	if len(packages) == 0 {
		return "", nil
	}
	return "RUN apt-get update -qq && apt-get install -qy " +
		strings.Join(packages, " ") +
		" && rm -rf /var/lib/apt/lists/*", nil
}

func (g *DockerfileGenerator) installPython() (string, error) {
	// TODO: check that python version is valid

	py := g.Config.Build.PythonVersion

	return `ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
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
` + fmt.Sprintf(`RUN curl https://pyenv.run | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s")`, py, py), nil
}

func (g *DockerfileGenerator) installCog() (string, error) {
	// Wheel name needs to be full format otherwise pip refuses to install it
	cogFilename := "cog-0.0.1.dev-py3-none-any.whl"
	cogPath := filepath.Join(g.Dir, ".cog/tmp", cogFilename)
	if err := os.MkdirAll(filepath.Dir(cogPath), 0755); err != nil {
		return "", fmt.Errorf("Failed to write %s: %w", cogFilename, err)
	}
	if err := os.WriteFile(cogPath, cogWheelEmbed, 0644); err != nil {
		return "", fmt.Errorf("Failed to write %s: %w", cogFilename, err)
	}
	g.generatedPaths = append(g.generatedPaths, cogPath)
	return fmt.Sprintf(`COPY .cog/tmp/%s /tmp/%s
RUN pip install /tmp/%s`, cogFilename, cogFilename, cogFilename), nil
}

func (g *DockerfileGenerator) pythonRequirements() (string, error) {
	reqs := g.Config.Build.PythonRequirements
	if reqs == "" {
		return "", nil
	}
	return fmt.Sprintf(`COPY %s /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt`, reqs), nil
}

func (g *DockerfileGenerator) pipInstalls() (string, error) {
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

	return "RUN pip install " + findLinks + " " + extraIndexURLs + " " + strings.Join(packages, " "), nil
}

func (g *DockerfileGenerator) copyCode() string {
	return `COPY . /src`
}

func (g *DockerfileGenerator) command() string {
	return `CMD ["python", "-m", "cog.server.http"]`
}

func (g *DockerfileGenerator) workdir() string {
	return "WORKDIR /src"
}

func (g *DockerfileGenerator) preInstall() string {
	lines := []string{}
	for _, run := range g.Config.Build.PreInstall {
		run = strings.TrimSpace(run)
		lines = append(lines, "RUN "+run)
	}
	return strings.Join(lines, "\n")
}

func label(name, value string) string {
	return fmt.Sprintf(`LABEL %s="%s"`, name, strings.Replace(value, `"`, `\"`, -1))
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
