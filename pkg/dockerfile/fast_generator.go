package dockerfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights"
)

const FUSE_RPC_WEIGHTS_PATH = "/srv/r8/fuse-rpc/weights"
const MONOBASE_CACHE_PATH = "/var/cache/monobase"
const APT_CACHE_MOUNT = "--mount=type=cache,target=/var/cache/apt,id=apt-cache"
const UV_CACHE_DIR = "/srv/r8/monobase/uv/cache"
const UV_CACHE_MOUNT = "--mount=type=cache,target=" + UV_CACHE_DIR + ",id=pip-cache"
const FAST_GENERATOR_NAME = "FAST_GENERATOR"

type FastGenerator struct {
	Config *config.Config
	Dir    string
}

func NewFastGenerator(config *config.Config, dir string) (*FastGenerator, error) {
	return &FastGenerator{
		Config: config,
		Dir:    dir,
	}, nil
}

func (g *FastGenerator) GenerateInitialSteps() (string, error) {
	return "", errors.New("GenerateInitialSteps not supported in FastGenerator")
}

func (g *FastGenerator) BaseImage() (string, error) {
	return "", errors.New("BaseImage not supported in FastGenerator")
}

func (g *FastGenerator) Cleanup() error {
	return nil
}

func (g *FastGenerator) GenerateDockerfileWithoutSeparateWeights() (string, error) {
	return g.generate()
}

func (g *FastGenerator) GenerateModelBase() (string, error) {
	return "", errors.New("GenerateModelBase not supported in FastGenerator")
}

func (g *FastGenerator) GenerateModelBaseWithSeparateWeights(imageName string) (weightsBase string, dockerfile string, dockerignoreContents string, err error) {
	return "", "", "", errors.New("GenerateModelBaseWithSeparateWeights not supported in FastGenerator")
}

func (g *FastGenerator) GenerateWeightsManifest() (*weights.Manifest, error) {
	return nil, errors.New("GenerateWeightsManifest not supported in FastGenerator")
}

func (g *FastGenerator) IsUsingCogBaseImage() bool {
	return false
}

func (g *FastGenerator) SetPrecompile(precompile bool) {
}

func (g *FastGenerator) SetStrip(strip bool) {
}

func (g *FastGenerator) SetUseCogBaseImage(useCogBaseImage bool) {
}

func (g *FastGenerator) SetUseCogBaseImagePtr(useCogBaseImage *bool) {
}

func (g *FastGenerator) SetUseCudaBaseImage(argumentValue string) {
}

func (g *FastGenerator) Name() string {
	return FAST_GENERATOR_NAME
}

func (g *FastGenerator) generate() (string, error) {
	tmpDir, err := BuildCogTempDir(g.Dir)
	if err != nil {
		return "", err
	}

	weights, err := FindWeights(g.Dir, tmpDir)
	if err != nil {
		return "", err
	}

	lines := []string{}
	lines, err = g.generateMonobase(lines, tmpDir)
	if err != nil {
		return "", err
	}

	lines, err = g.copyWeights(lines, weights)
	if err != nil {
		return "", err
	}

	lines, err = g.install(lines, weights, tmpDir)
	if err != nil {
		return "", err
	}

	lines, err = g.entrypoint(lines)
	if err != nil {
		return "", err
	}

	return strings.Join(lines, "\n"), nil
}

func (g *FastGenerator) copyCog(tmpDir string) (string, error) {
	files, err := CogEmbed.ReadDir("embed")
	if err != nil {
		return "", err
	}
	if len(files) != 1 {
		return "", fmt.Errorf("should only have one cog wheel embedded")
	}
	filename := files[0].Name()
	data, err := CogEmbed.ReadFile("embed/" + filename)
	if err != nil {
		return "", err
	}
	path := filepath.Join(tmpDir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("Failed to write %s: %w", filename, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("Failed to write %s: %w", filename, err)
	}
	return path, nil
}

func (g *FastGenerator) generateMonobase(lines []string, tmpDir string) ([]string, error) {
	lines = append(lines, []string{
		"# syntax=docker/dockerfile:1-labs",
		"FROM r8.im/monobase:latest",
	}...)

	cogPath, err := g.copyCog(tmpDir)
	if err != nil {
		return nil, err
	}

	lines = append(lines, []string{
		"ENV R8_COG_VERSION=\"file:///buildtmp/" + filepath.Base(cogPath) + "\"",
	}...)

	if g.Config.Build.GPU {
		cudaVersion := g.Config.Build.CUDA
		cudnnVersion := g.Config.Build.CuDNN
		lines = append(lines, []string{
			"ENV R8_CUDA_VERSION=" + cudaVersion,
			"ENV R8_CUDNN_VERSION=" + cudnnVersion,
			"ENV R8_CUDA_PREFIX=https://monobase.replicate.delivery/cuda",
			"ENV R8_CUDNN_PREFIX=https://monobase.replicate.delivery/cudnn",
		}...)
	}

	lines = append(lines, []string{
		"ENV R8_PYTHON_VERSION=" + g.Config.Build.PythonVersion,
	}...)

	torchVersion, ok := g.Config.TorchVersion()
	if ok {
		lines = append(lines, []string{
			"ENV R8_TORCH_VERSION=" + torchVersion,
		}...)
	}

	buildTmpMount, err := g.buildTmpMount(tmpDir)
	if err != nil {
		return nil, err
	}

	return append(lines, []string{
		"RUN " + strings.Join([]string{
			buildTmpMount,
			g.monobaseUsercacheMount(),
			APT_CACHE_MOUNT,
			UV_CACHE_MOUNT,
		}, " ") + " UV_CACHE_DIR=\"" + UV_CACHE_DIR + "\" UV_LINK_MODE=copy /opt/r8/monobase/run.sh monobase.build --mini --cache=" + MONOBASE_CACHE_PATH,
	}...), nil
}

func (g *FastGenerator) copyWeights(lines []string, weights []Weight) ([]string, error) {
	if len(weights) == 0 {
		return lines, nil
	}

	for _, weight := range weights {
		lines = append(lines, "COPY --link \""+weight.Path+"\" \""+filepath.Join(FUSE_RPC_WEIGHTS_PATH, weight.Digest)+"\"")
	}

	return lines, nil
}

func (g *FastGenerator) install(lines []string, weights []Weight, tmpDir string) ([]string, error) {
	// Install apt packages
	packages := g.Config.Build.SystemPackages
	if len(packages) > 0 {
		lines = append(lines, "RUN "+APT_CACHE_MOUNT+" apt-get update && apt-get install -qqy "+strings.Join(packages, " ")+" && rm -rf /var/lib/apt/lists/*")
	}

	// Install python packages
	requirementsFile, err := g.pythonRequirements(tmpDir)
	if err != nil {
		return nil, err
	}
	buildTmpMount, err := g.buildTmpMount(tmpDir)
	if err != nil {
		return nil, err
	}
	if requirementsFile != "" {
		lines = append(lines, "RUN "+strings.Join([]string{
			buildTmpMount,
			UV_CACHE_MOUNT,
		}, " ")+" UV_CACHE_DIR=\""+UV_CACHE_DIR+"\" UV_LINK_MODE=copy UV_COMPILE_BYTECODE=0 /opt/r8/monobase/run.sh monobase.user --requirements=/buildtmp/requirements.txt")
	}

	// Copy over source / without weights
	copyCommand := "COPY --link "
	for _, weight := range weights {
		copyCommand += "--exclude='" + weight.Path + "' "
	}
	copyCommand += ". /src"
	lines = append(lines, copyCommand)

	// Link to weights
	if len(weights) > 0 {
		linkCommands := []string{}
		for _, weight := range weights {
			linkCommands = append(linkCommands, "ln -s \""+filepath.Join(FUSE_RPC_WEIGHTS_PATH, weight.Digest)+"\" \"/src/"+weight.Path+"\"")
		}
		lines = append(lines, "RUN "+strings.Join(linkCommands, " && "))
	}

	return lines, nil
}

func (g *FastGenerator) pythonRequirements(tmpDir string) (string, error) {
	return config.GenerateRequirements(tmpDir, g.Config)
}

func (g *FastGenerator) entrypoint(lines []string) ([]string, error) {
	return append(lines, []string{
		"WORKDIR /src",
		"ENV VERBOSE=0",
		"ENTRYPOINT [\"/usr/bin/tini\", \"--\", \"/opt/r8/monobase/exec.sh\"]",
		"CMD [\"python\", \"-m\", \"cog.server.http\"]",
	}...), nil
}

func (g *FastGenerator) buildTmpMount(tmpDir string) (string, error) {
	relativeTmpDir, err := filepath.Rel(g.Dir, tmpDir)
	if err != nil {
		return "", err
	}
	return "--mount=type=bind,ro,source=\"" + relativeTmpDir + "\",target=\"/buildtmp\"", nil
}

func (g *FastGenerator) monobaseUsercacheMount() string {
	return "--mount=type=cache,from=usercache,target=\"" + MONOBASE_CACHE_PATH + "\""
}
