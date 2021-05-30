package docker

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/segmentio/ksuid"

	"github.com/replicate/cog/pkg/model"
)

const SectionPrefix = "### --> "

const (
	SectionStartingBuild                 = "Starting build"
	SectionInstallingSystemPackages      = "Installing system packages"
	SectionInstallingPythonPrerequisites = "Installing Python prerequisites"
	SectionInstallingPython              = "Installing Python"
	SectionInstallingPythonRequirements  = "Installing Python requirements"
	SectionInstallingPythonPackages      = "Installing Python packages"
	SectionInstallingCog                 = "Installing Cog"
	SectionCopyingCode                   = "Copying code"
	SectionPreInstall                    = "Running pre-install script"
)

//go:embed cog.py
var cogLibrary []byte

type DockerfileGenerator struct {
	Config *model.Config
	Arch   string
	Dir    string

	// these are here to make this type testable
	GOOS   string
	GOARCH string

	// to clean up
	generatedPaths []string
}

func NewDockerfileGenerator(config *model.Config, arch string, dir string) *DockerfileGenerator {
	return &DockerfileGenerator{
		Config:         config,
		Arch:           arch,
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
	installPython, err := g.installPython()
	if err != nil {
		return "", err
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
	return strings.Join(filterEmpty([]string{
		"FROM " + baseImage,
		g.preamble(),
		installPython,
		aptInstalls,
		pythonRequirements,
		pipInstalls,
		installCog,
		g.preInstall(),
		g.workdir(),
	}), "\n"), nil
}

func (g *DockerfileGenerator) Generate() (string, error) {
	base, err := g.GenerateBase()
	if err != nil {
		return "", err
	}
	return strings.Join(filterEmpty([]string{
		base,
		g.installHelperScripts(),
		g.copyCode(),
		g.command(),
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
	switch g.Arch {
	case "cpu":
		return "ubuntu:20.04", nil
	case "gpu":
		return g.Config.CUDABaseImageTag()
	}
	return "", fmt.Errorf("Invalid architecture: %s", g.Arch)
}

func (g *DockerfileGenerator) preamble() string {
	// TODO: other stuff
	return `ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu`
}

func (g *DockerfileGenerator) aptInstalls() (string, error) {
	packages := g.Config.Environment.SystemPackages
	if len(packages) == 0 {
		return "", nil
	}
	return g.sectionLabel(SectionInstallingSystemPackages) + "RUN apt-get update -qq && apt-get install -qy " +
		strings.Join(packages, " ") +
		" && rm -rf /var/lib/apt/lists/*", nil
}

func (g *DockerfileGenerator) installPython() (string, error) {
	// TODO: check that python version is valid

	py := g.Config.Environment.PythonVersion

	return g.sectionLabel(SectionInstallingPythonPrerequisites) + `ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
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
` + g.sectionLabel(SectionInstallingPython+" "+g.Config.Environment.PythonVersion) + fmt.Sprintf(`RUN curl https://pyenv.run | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s")`, py, py), nil
}

func (g *DockerfileGenerator) installCog() (string, error) {
	cogFilename := "cog." + ksuid.New().String() + ".py"
	cogPath := filepath.Join(g.Dir, cogFilename)
	if err := os.WriteFile(cogPath, cogLibrary, 0644); err != nil {
		return "", fmt.Errorf("Failed to write cog.py: %w", err)
	}
	g.generatedPaths = append(g.generatedPaths, cogPath)
	return g.sectionLabel(SectionInstallingCog) +
		fmt.Sprintf(`RUN pip install flask requests redis
ENV PYTHONPATH=/usr/local/lib/cog
RUN mkdir -p /usr/local/lib/cog
COPY %s /usr/local/lib/cog/cog.py`, cogFilename), nil
}

func (g *DockerfileGenerator) installHelperScripts() string {
	return g.serverHelperScript("HTTPServer", "cog-http-server") +
		g.serverHelperScript("AIPlatformPredictionServer", "cog-ai-platform-prediction-server") +
		g.queueWorkerHelperScript()
}

func (g *DockerfileGenerator) serverHelperScript(serverClass string, filename string) string {
	scriptPath := "/usr/bin/" + filename
	name := g.Config.Model
	parts := strings.Split(name, ".py:")
	module := parts[0]
	class := parts[1]
	script := `#!/usr/bin/env python
import sys
import cog
import os
os.chdir("` + g.getWorkdir() + `")
sys.path.append("` + g.getWorkdir() + `")
from ` + module + ` import ` + class + `
cog.` + serverClass + `(` + class + `()).start_server()`
	scriptString := strings.ReplaceAll(script, "\n", "\\n")
	return `
RUN echo '` + scriptString + `' > ` + scriptPath + `
RUN chmod +x ` + scriptPath
}

func (g *DockerfileGenerator) queueWorkerHelperScript() string {
	scriptPath := "/usr/bin/cog-redis-queue-worker"
	name := g.Config.Model
	parts := strings.Split(name, ".py:")
	module := parts[0]
	class := parts[1]
	script := `#!/usr/bin/env python
import sys
import cog
import os
os.chdir("` + g.getWorkdir() + `")
sys.path.append("` + g.getWorkdir() + `")
from ` + module + ` import ` + class + `
cog.RedisQueueWorker(` + class + `(), redis_host=sys.argv[1], redis_port=sys.argv[2], input_queue=sys.argv[3], upload_url=sys.argv[4], consumer_id=sys.argv[5]).start()`
	scriptString := strings.ReplaceAll(script, "\n", "\\n")
	return `
RUN echo '` + scriptString + `' > ` + scriptPath + `
RUN chmod +x ` + scriptPath
}

func (g *DockerfileGenerator) pythonRequirements() (string, error) {
	reqs := g.Config.Environment.PythonRequirements
	if reqs == "" {
		return "", nil
	}
	return g.sectionLabel(SectionInstallingPythonRequirements) + fmt.Sprintf(`COPY %s /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt`, reqs), nil
}

func (g *DockerfileGenerator) pipInstalls() (string, error) {
	packages, indexURLs, err := g.Config.PythonPackagesForArch(g.Arch, g.GOOS, g.GOARCH)
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
	for _, indexURL := range g.Config.Environment.PythonFindLinks {
		findLinks += "-f " + indexURL + " "
	}
	extraIndexURLs := ""
	for _, indexURL := range g.Config.Environment.PythonExtraIndexURLs {
		extraIndexURLs += "--extra-index-url=" + indexURL
	}

	return g.sectionLabel(SectionInstallingPythonPackages) + "RUN pip install " + findLinks + " " + extraIndexURLs + " " + strings.Join(packages, " "), nil
}

func (g *DockerfileGenerator) copyCode() string {
	return g.sectionLabel(SectionCopyingCode) + `COPY . /code`
}

func (g *DockerfileGenerator) command() string {
	// TODO: handle predict scripts in subdirectories
	// TODO: check this actually exists
	return `CMD /usr/bin/cog-http-server`
}

func (g *DockerfileGenerator) workdir() string {
	return "WORKDIR " + g.getWorkdir()
}

func (g *DockerfileGenerator) getWorkdir() string {
	wd := "/code"
	if g.Config.Workdir != "" {
		wd += "/" + g.Config.Workdir
	}
	return wd
}

func (g *DockerfileGenerator) preInstall() string {
	lines := []string{}
	for _, run := range g.Config.Environment.PreInstall {
		run = strings.TrimSpace(run)
		lines = append(lines, g.sectionLabel(SectionPreInstall+" "+run)+"RUN "+run)
	}
	return strings.Join(lines, "\n")
}

func (g *DockerfileGenerator) sectionLabel(label string) string {
	return fmt.Sprintf("RUN %s%s\n", SectionPrefix, label)
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
