package dockerfile

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/weights"
)

const FUSE_RPC_WEIGHTS_PATH = "/srv/r8/fuse-rpc/weights"
const MONOBASE_CACHE_PATH = "/var/cache/monobase"
const APT_CACHE_MOUNT = "--mount=type=cache,target=/var/cache/apt,id=apt-cache,sharing=locked"
const UV_CACHE_DIR = "/srv/r8/monobase/uv/cache"
const UV_CACHE_MOUNT = "--mount=type=cache,target=" + UV_CACHE_DIR + ",id=pip-cache"
const FAST_GENERATOR_NAME = "FAST_GENERATOR"

type FastGenerator struct {
	Config  *config.Config
	Dir     string
	command docker.Command
	matrix  MonobaseMatrix
}

type MonobaseMatrix struct {
	Id             int            `json:"id"`
	CudaVersions   []string       `json:"cuda_versions"`
	CudnnVersions  []string       `json:"cudnn_versions"`
	PythonVersions []string       `json:"python_versions"`
	TorchVersions  []string       `json:"torch_versions"`
	Venvs          []MonobaseVenv `json:"venvs"`
}

type MonobaseVenv struct {
	Python string `json:"python"`
	Torch  string `json:"torch"`
	Cuda   string `json:"cuda"`
}

func (m MonobaseMatrix) DefaultCudnnVersion() string {
	slices.SortFunc(m.CudnnVersions, func(s1, s2 string) int {
		i1, e1 := strconv.Atoi(s1)
		i2, e2 := strconv.Atoi(s2)
		if e1 != nil || e2 != nil {
			return strings.Compare(s1, s2)
		}
		return cmp.Compare(i1, i2)
	})
	return m.CudnnVersions[len(m.CudnnVersions)-1]
}

func (m MonobaseMatrix) IsSupported(python string, torch string, cuda string) bool {
	if torch == "" {
		return slices.Contains(m.PythonVersions, python)
	}
	if cuda == "" {
		cuda = "cpu"
	}
	return slices.Contains(m.Venvs, MonobaseVenv{Python: python, Torch: torch, Cuda: cuda})
}

func NewFastGenerator(config *config.Config, dir string, command docker.Command) (*FastGenerator, error) {
	resp, err := http.DefaultClient.Get("https://raw.githubusercontent.com/replicate/monobase/refs/heads/main/matrix.json")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Failed to fetch Monobase support matrix")
	}
	var matrix MonobaseMatrix
	if err := json.NewDecoder(resp.Body).Decode(&matrix); err != nil {
		return nil, err
	}

	return &FastGenerator{
		Config:  config,
		Dir:     dir,
		command: command,
		matrix:  matrix,
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

	weights, err := weights.FindFastWeights(g.Dir, tmpDir)
	if err != nil {
		return "", err
	}

	aptTarFile, err := g.generateAptTarball(tmpDir)
	if err != nil {
		return "", fmt.Errorf("generate apt tarball: %w", err)
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

	lines, err = g.install(lines, weights, tmpDir, aptTarFile)
	if err != nil {
		return "", err
	}

	lines, err = g.entrypoint(lines)
	if err != nil {
		return "", err
	}

	return strings.Join(lines, "\n"), nil
}

func (g *FastGenerator) generateMonobase(lines []string, tmpDir string) ([]string, error) {
	lines = append(lines, []string{
		"# syntax=docker/dockerfile:1-labs",
		"FROM r8.im/monobase:latest",
	}...)

	lines = append(lines, []string{
		// This installs latest version of coglet
		"ENV R8_COG_VERSION=coglet",
	}...)

	if g.Config.Build.GPU {
		cudaVersion := g.Config.Build.CUDA
		cudnnVersion := g.Config.Build.CuDNN
		if cudnnVersion == "" {
			cudnnVersion = g.matrix.DefaultCudnnVersion()
		}
		if !CheckMajorMinorOnly(cudaVersion) {
			return nil, fmt.Errorf("CUDA version must be <major>.<minor>, supported versions: %s", strings.Join(g.matrix.CudaVersions, ", "))
		}
		if !CheckMajorOnly(cudnnVersion) {
			return nil, fmt.Errorf("CUDNN version must be <major> only, supported versions: %s", strings.Join(g.matrix.CudnnVersions, ", "))
		}

		lines = append(lines, []string{
			"ENV R8_CUDA_VERSION=" + cudaVersion,
			"ENV R8_CUDNN_VERSION=" + cudnnVersion,
			"ENV R8_CUDA_PREFIX=https://monobase.replicate.delivery/cuda",
			"ENV R8_CUDNN_PREFIX=https://monobase.replicate.delivery/cudnn",
		}...)
	}

	if !CheckMajorMinorOnly(g.Config.Build.PythonVersion) {
		return nil, fmt.Errorf(
			"Python version must be <major>.<minor>, supported versions: %s", strings.Join(g.matrix.PythonVersions, ", "))
	}
	lines = append(lines, []string{
		"ENV R8_PYTHON_VERSION=" + g.Config.Build.PythonVersion,
	}...)

	torchVersion, ok := g.Config.TorchVersion()
	if ok {
		if !CheckMajorMinorPatch(torchVersion) {
			return nil, fmt.Errorf("Torch version must be <major>.<minor>.<patch>: %s", strings.Join(g.matrix.TorchVersions, ", "))
		}
		lines = append(lines, []string{
			"ENV R8_TORCH_VERSION=" + torchVersion,
		}...)
	}

	if !g.matrix.IsSupported(g.Config.Build.PythonVersion, torchVersion, g.Config.Build.CUDA) {
		return nil, fmt.Errorf(
			"Unsupported version combination: Python=%s, Torch=%s, CUDA=%s",
			g.Config.Build.PythonVersion, torchVersion, g.Config.Build.CUDA)
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

func (g *FastGenerator) copyWeights(lines []string, weights []weights.Weight) ([]string, error) {
	if len(weights) == 0 {
		return lines, nil
	}

	for _, weight := range weights {
		lines = append(lines, "COPY --link \""+weight.Path+"\" \""+filepath.Join(FUSE_RPC_WEIGHTS_PATH, weight.Digest)+"\"")
	}

	return lines, nil
}

func (g *FastGenerator) install(lines []string, weights []weights.Weight, tmpDir string, aptTarFile string) ([]string, error) {
	// Install apt packages
	buildTmpMount, err := g.buildTmpMount(tmpDir)
	if err != nil {
		return nil, err
	}
	if aptTarFile != "" {
		lines = append(lines, "RUN "+buildTmpMount+" tar -xf \""+filepath.Join("/buildtmp", aptTarFile)+"\" -C /")
	}

	// Install python packages
	requirementsFile, err := g.pythonRequirements(tmpDir)
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
	copyCommand := "COPY --link --exclude='.cog' "
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
	return requirements.GenerateRequirements(tmpDir, g.Config)
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

func (g *FastGenerator) generateAptTarball(tmpDir string) (string, error) {
	return docker.CreateAptTarball(tmpDir, g.command, g.Config.Build.SystemPackages...)
}
