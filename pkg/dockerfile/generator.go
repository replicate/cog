package dockerfile

import (
	// blank import for embeds
	_ "embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	if err := os.MkdirAll(rootTmp, 0755); err != nil {
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
		g.workdir(),
		g.command(),
	}), "\n"), nil
}

func (g *Generator) Generate() (string, error) {
	base, err := g.GenerateBase()
	if err != nil {
		return "", err
	}
	return strings.Join(filterEmpty([]string{
		base,
		g.copyCode(),
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
	environmentVariables := g.Config.Build.EnvironmentVariables

	// Regex for valid environment variable names
	regexpVariableName := regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")

	// Variables should be a list of strings, formatted like `KEY=VALUE`.
	// Parse them into a dict.
	dict := make(map[string]string)
	if len(environmentVariables) > 0 {
		dict = make(map[string]string)
		for _, v := range environmentVariables {
			parts := strings.SplitN(v, "=", 2)
			if len(parts) != 2 {
				// FIXME: This should log-warning/hint instead of returning junk.
				return fmt.Sprintf("# ignoring invalid variable: %s", v)
			}

			// Use a regex to limit the characters allowed in the key.
			if ok := regexpVariableName.MatchString(parts[0]); !ok {
				// FIXME: This should log-warning/hint instead of returning junk.
				parts[0] = "__BAD_FORMAT__"
				return fmt.Sprintf("# ignoring invalid variable: %s", v)
			}

			dict[parts[0]] = parts[1]
		}
	}

	if _, ok := dict["XDG_CACHE_HOME"]; !ok {
		// Cog sets a default value for $XDG_CACHE_HOME. Why:
		// Cog mounts the project directory so anything in there (in /src in the
		// image) gets retained between runs. $XDG_CACHE_HOME is used by various
		// libraries including popular ML libraries; so, setting it to a subdirectory
		// within /src results in retaining the cache between runs. Ultimately,
		// everything in /src gets "baked in" to the cog image. So this is great
		// for caching pretrained models and so on.
		// For more context see: https://github.com/replicate/cog/issues/320
		dict["XDG_CACHE_HOME"] = "/src/cog_cache_home"
	}

	// TODO <DELETE>
	// 	// Format the variables into a list of `ENV KEY=VALUE` strings.
	// 	// TODO(optimization): It should do first item ENV then the rest "\", as below.
	// 	// (................): Why: each "ENV" directive creates a layer.
	// 	var envVarLines []string
	// 	for k, v := range dict {
	// 		envVarLines = append(envVarLines, fmt.Sprintf("ENV %s=%s", k, v))
	// 	}

	// 	return `
	// ENV DEBIAN_FRONTEND=noninteractive \
	// 		PYTHONUNBUFFERED=1 \
	// 		LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin
	// ` + strings.Join(envVarLines, "\n")
	// }
	// TODO </DELETE>

	// Format the variables into a list of `ENV KEY=VALUE` strings.
	// TODO(optimization): It should do first item ENV then the rest "\", as below.
	// (................): Why: each "ENV" directive creates a layer.
	var envVarLines []string
	for k, v := range dict {
		envVarLines = append(envVarLines, fmt.Sprintf("\t%s=%s", k, v))
	}

	return `ENV DEBIAN_FRONTEND=noninteractive \
	PYTHONUNBUFFERED=1 \
	LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib64:/usr/local/nvidia/bin \
` + strings.Join(envVarLines, " \\"+"\n")
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
` + fmt.Sprintf(`RUN curl https://pyenv.run | bash && \
	git clone https://github.com/momo-lab/pyenv-install-latest.git "$(pyenv root)"/plugins/pyenv-install-latest && \
	pyenv install-latest "%s" && \
	pyenv global $(pyenv install-latest --print "%s")`, py, py), nil
}

func (g *Generator) installCog() (string, error) {
	// Wheel name needs to be full format otherwise pip refuses to install it
	cogFilename := "cog-0.0.1.dev-py3-none-any.whl"
	cogPath := filepath.Join(g.tmpDir, cogFilename)
	if err := os.MkdirAll(filepath.Dir(cogPath), 0755); err != nil {
		return "", fmt.Errorf("Failed to write %s: %w", cogFilename, err)
	}
	if err := os.WriteFile(cogPath, cogWheelEmbed, 0644); err != nil {
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

func (g *Generator) copyCode() string {
	return `COPY . /src`
}

func (g *Generator) command() string {
	return `CMD ["python", "-m", "cog.server.http"]`
}

func (g *Generator) workdir() string {
	return "WORKDIR /src"
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
