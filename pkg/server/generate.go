package server

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/replicate/cog/pkg/model"
)

const codeDir = "/code"

//go:embed cog.py
var cogLibrary []byte

type DockerfileGenerator struct {
	Config *model.Config
	Arch   string
}

func (g *DockerfileGenerator) Generate() (string, error) {
	baseImage, err := g.baseImage()
	if err != nil {
		return "", err
	}
	preamble := g.preamble()
	aptInstalls, err := g.aptInstalls()
	if err != nil {
		return "", err
	}
	installPython, err := g.installPython()
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
	return strings.Join(filterEmpty([]string{
		"FROM " + baseImage,
		preamble,
		aptInstalls,
		installPython,
		pythonRequirements,
		pipInstalls,
		g.installCog(),
		g.copyCode(),
		g.command(),
	}), "\n"), nil
}

func (g *DockerfileGenerator) baseImage() (string, error) {
	switch g.Arch {
	case "cpu":
		return "ubuntu:20.04", nil
	case "gpu":
		return g.gpuBaseImage()
	}
	return "", fmt.Errorf("Invalid architecture: %s", g.Arch)
}

func (g *DockerfileGenerator) preamble() string {
	// TODO: other stuff
	return "ENV DEBIAN_FRONTEND=noninteractive"
}

func (g *DockerfileGenerator) gpuBaseImage() (string, error) {
	// TODO: return correct ubuntu version for tf / torch
	return "nvidia/cuda:11.0-devel-ubuntu20.04", nil
}

func (g *DockerfileGenerator) aptInstalls() (string, error) {
	packages := g.Config.Environment.SystemPackages
	if len(packages) == 0 {
		return "", nil
	}
	return "RUN apt-get update -qq && apt-get install -qy " +
		strings.Join(packages, " ") +
		" && rm -rf /var/lib/apt/lists/*", nil
}

func (g *DockerfileGenerator) installPython() (string, error) {
	// TODO: check that python version is valid

	py := g.Config.Environment.PythonVersion

	return fmt.Sprintf(`
ENV PATH="/root/.pyenv/shims:/root/.pyenv/bin:$PATH"
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
	pyenv global $(pyenv install-latest --print "%s")`, py, py), nil
}

func (g *DockerfileGenerator) installCog() string {
	cogLibB64 := base64.StdEncoding.EncodeToString(cogLibrary)
	return fmt.Sprintf(`RUN pip install flask
ENV PYTHONPATH=/usr/local/lib/cog
RUN mkdir -p /usr/local/lib/cog && echo %s | base64 --decode > /usr/local/lib/cog/cog.py`, cogLibB64)
}

func (g *DockerfileGenerator) pythonRequirements() (string, error) {
	reqs := g.Config.Environment.PythonRequirements
	if reqs == "" {
		return "", nil
	}
	return fmt.Sprintf(`COPY %s /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt`, reqs), nil
}

func (g *DockerfileGenerator) pipInstalls() (string, error) {
	packages, indexURLs, err := g.Config.PythonPackagesForArch(g.Arch)
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

	return "RUN pip install " + findLinks + " " + extraIndexURLs + " " + strings.Join(packages, " "), nil
}

func (g *DockerfileGenerator) copyCode() string {
	return `COPY . /code
WORKDIR /code`
}

func (g *DockerfileGenerator) command() string {
	// TODO: handle infer scripts in subdirectories
	// TODO: check this actually exists
	name := g.Config.Model
	parts := strings.Split(name, ".py:")
	module := parts[0]
	class := parts[1]
	return `CMD ["python", "-c", "from ` + module + ` import ` + class + `; ` + class + `().start_server()"]`
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
