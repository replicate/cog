package server

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"
)

const codeDir = "/code"

//go:embed jid.py
var jidLibrary []byte

type DockerfileGenerator struct {
	Config *Config
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
		g.installJid(),
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
	packages := append(g.Config.Environment.SystemPackages, "curl")
	return "RUN apt-get update && apt-get install -y " +
		strings.Join(packages, " ") +
		" && rm -rf /var/lib/apt/lists/*", nil
}

func (g *DockerfileGenerator) installPython() (string, error) {
	// TODO: check that python version is valid

	py := g.Config.Environment.PythonVersion
	pyMajor := strings.Split(py, ".")[0]

	return fmt.Sprintf(`RUN apt-get update \
	&& apt-get install -y --no-install-recommends software-properties-common \
	&& add-apt-repository -y ppa:deadsnakes/ppa \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends python%s python%s-pip \
	&& apt-get purge -y --auto-remove software-properties-common \
	&& rm -rf /var/lib/apt/lists/* \
	&& ln -s /usr/bin/python%s /usr/bin/python \
	&& ln -s /usr/bin/pip%s /usr/bin/pip`, py, pyMajor, py, pyMajor), nil
}

func (g *DockerfileGenerator) installJid() string {
	jidLibB64 := base64.StdEncoding.EncodeToString(jidLibrary)
	return fmt.Sprintf(`RUN pip install flask
RUN curl -L https://github.com/mikefarah/yq/releases/download/v4.6.1/yq_linux_amd64.tar.gz | tar -xzO > /usr/bin/yq
RUN echo %s | base64 --decode > /usr/local/lib/python%s/dist-packages/jid.py`, jidLibB64, g.Config.Environment.PythonVersion)
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
	packages := g.Config.Environment.PythonPackages
	if len(packages) == 0 {
		return "", nil
	}

	return "RUN pip install " + strings.Join(packages, " "), nil
}

func (g *DockerfileGenerator) copyCode() string {
	return `COPY . /code
WORKDIR /code`
}

func (g *DockerfileGenerator) command() string {
	// TODO: handle infer scripts in subdirectories
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
