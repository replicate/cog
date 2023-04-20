package dockerfile

import (
	// blank import for embeds
	_ "embed"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/replicate/cog/pkg/config"
)

//go:embed embed/cog.whl
var cogWheelEmbed []byte

const (
	maxNumFileGroups  = 3
	fileSizeThresHold = 100 * 1000 * 1000 // 100 MegaBytes
)

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
	// groupFile indicates grouping small files into independent docker
	// image layer
	groupFile bool
}

func NewGenerator(config *config.Config, dir string, groupFile bool) (*Generator, error) {
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
		groupFile:      groupFile,
	}, nil
}

func (g *Generator) GenerateBase() (string, error) {
	baseImage, err := g.baseImage()
	if err != nil {
		return "", err
	}
	installPython := ""
	if g.Config.Build.GPU {
		installPython, err = g.installPythonCUDA()
		if err != nil {
			return "", err
		}
	}
	aptInstalls, err := g.aptInstalls()
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
		g.installTini(),
		installPython,
		installCog,
		aptInstalls,
		pipInstalls,
		run,
		`WORKDIR /src`,
		`EXPOSE 5000`,
		`CMD ["python", "-m", "cog.server.http"]`,
	}), "\n"), nil
}

func dirSize(dir string) (int64, error) {
	var size int64
	if err := filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				size += info.Size()
			}
			return nil
		},
	); err != nil {
		return 0, err
	}
	return size, nil
}

func divFilesBySize(threshold int64) (smalls []string, larges []string, err error) {
	files, err := ioutil.ReadDir(".")
	if err != nil {
		return nil, nil, err
	}

	for _, file := range files {
		size := file.Size()
		if file.IsDir() {
			size, err = dirSize(file.Name())
			if err != nil {
				return nil, nil, err
			}
		}
		if size < threshold {
			// check if file size is smaller than 100 MB
			smalls = append(smalls, file.Name())
			continue
		}
		larges = append(larges, file.Name())
	}
	return
}

func groupFiles(numGroups int) ([][]string, error) {
	smalls, larges, err := divFilesBySize(fileSizeThresHold)
	if err != nil {
		return nil, err
	}
	ret := [][]string{}
	nf := len(smalls)
	if len(smalls) <= numGroups {
		// put each file in one group
		for _, f := range smalls {
			ret = append(ret, []string{f})
		}
		return ret, nil
	}
	// TODO(charleszheng44): Improvement
	filePerGroup, i := nf/numGroups, 0
	for q := 0; q < numGroups; q++ {
		curGrp := []string{}
		for j := 0; j < filePerGroup; j, i = j+1, i+1 {
			curGrp = append(curGrp, smalls[i])
		}
		ret = append(ret, curGrp)
	}
	// put the reminders into the last group.
	ret[numGroups-1] = append(ret[numGroups-1], smalls[i:]...)
	// put all large files in an independent group.
	ret = append(ret, larges)
	return ret, nil
}

func (g *Generator) copyWorkspace() (string, error) {
	if !g.groupFile {
		return "COPY . /src", nil
	}

	ret := ""
	groups, err := groupFiles(maxNumFileGroups)
	if err != nil {
		return "", err
	}

	for _, group := range groups {
		copyCmd := "COPY "
		for _, file := range group {
			copyCmd = copyCmd + file + " "
		}
		copyCmd = copyCmd + "/src" + "\n"
		ret = ret + copyCmd
	}
	return ret, nil
}

func (g *Generator) Generate() (string, error) {
	base, err := g.GenerateBase()
	if err != nil {
		return "", err
	}

	copyWorkspace, err := g.copyWorkspace()
	if err != nil {
		return "", err
	}

	return strings.Join(filterEmpty(
		[]string{
			base,
			copyWorkspace,
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

func (g *Generator) installTini() string {
	// Install tini as the image entrypoint to provide signal handling and process
	// reaping appropriate for PID 1.
	//
	// N.B. If you remove/change this, consider removing/changing the `has_init`
	// image label applied in image/build.go.
	lines := []string{
		`RUN --mount=type=cache,target=/var/cache/apt set -eux; \
apt-get update -qq; \
apt-get install -qqy --no-install-recommends curl; \
rm -rf /var/lib/apt/lists/*; \
TINI_VERSION=v0.19.0; \
TINI_ARCH="$(dpkg --print-architecture)"; \
curl -sSL -o /sbin/tini "https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-${TINI_ARCH}"; \
chmod +x /sbin/tini`,
		`ENTRYPOINT ["/sbin/tini", "--"]`,
	}
	return strings.Join(lines, "\n")
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

func (g *Generator) installPythonCUDA() (string, error) {
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
	lines, containerPath, err := g.writeTemp(cogFilename, cogWheelEmbed)
	if err != nil {
		return "", err
	}
	lines = append(lines, fmt.Sprintf("RUN --mount=type=cache,target=/root/.cache/pip pip install %s", containerPath))
	return strings.Join(lines, "\n"), nil
}

func (g *Generator) pipInstalls() (string, error) {
	requirements, err := g.Config.PythonRequirementsForArch(g.GOOS, g.GOARCH)
	if err != nil {
		return "", err
	}
	if strings.Trim(requirements, "") == "" {
		return "", nil
	}

	lines, containerPath, err := g.writeTemp("requirements.txt", []byte(requirements))
	if err != nil {
		return "", err
	}

	lines = append(lines, "RUN --mount=type=cache,target=/root/.cache/pip pip install -r "+containerPath)
	return strings.Join(lines, "\n"), nil
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

// writeTemp writes a temporary file that can be used as part of the build process
// It returns the lines to add to Dockerfile to make it available and the filename it ends up as inside the container
func (g *Generator) writeTemp(filename string, contents []byte) ([]string, string, error) {
	path := filepath.Join(g.tmpDir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return []string{}, "", fmt.Errorf("Failed to write %s: %w", filename, err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return []string{}, "", fmt.Errorf("Failed to write %s: %w", filename, err)
	}
	return []string{fmt.Sprintf("COPY %s /tmp/%s", filepath.Join(g.relativeTmpDir, filename), filename)}, "/tmp/" + filename, nil
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
