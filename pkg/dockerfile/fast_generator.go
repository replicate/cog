package dockerfile

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights"
)

const FUSE_RPC_WEIGHTS_PATH = "/srv/r8/fuse-rpc/weights"
const MONOBASE_CACHE_PATH = "/var/cache/monobase"

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
	return "", nil
}

func (g *FastGenerator) BaseImage() (string, error) {
	return "", nil
}

func (g *FastGenerator) Cleanup() error {
	return nil
}

func (g *FastGenerator) GenerateDockerfileWithoutSeparateWeights() (string, error) {
	return g.generate()
}

func (g *FastGenerator) GenerateModelBase() (string, error) {
	return "", nil
}

func (g *FastGenerator) GenerateModelBaseWithSeparateWeights(imageName string) (weightsBase string, dockerfile string, dockerignoreContents string, err error) {
	return "", "", "", nil
}

func (g *FastGenerator) GenerateWeightsManifest() (*weights.Manifest, error) {
	return nil, nil
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

func (g *FastGenerator) generate() (string, error) {
	tmpDir, err := BuildTempDir(g.Dir)
	if err != nil {
		return "", err
	}

	lines := []string{}
	lines, err = g.generateMonobase(lines, tmpDir)
	if err != nil {
		return "", err
	}

	lines, err = g.copyWeights(lines, tmpDir)
	if err != nil {
		return "", err
	}

	lines, err = g.install(lines)
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
		"FROM r8.im/monobase:latest",
	}...)

	cogPath, err := g.copyCog(tmpDir)
	if err != nil {
		return nil, err
	}

	lines = append(lines, []string{
		"ENV R8_COG_VERSION=\"file:///buildtmp/" + filepath.Base(cogPath) + "\"",
	}...)

	relativeTmpDir, err := filepath.Rel(g.Dir, tmpDir)
	if err != nil {
		return nil, err
	}
	skipCudaArg := "--skip-cuda"
	if g.Config.Build.GPU {
		skipCudaArg = ""
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

	return append(lines, []string{
		"RUN --mount=type=bind,source=\"" + relativeTmpDir + "\",target=/buildtmp --mount=type=cache,from=usercache,target=\"" + MONOBASE_CACHE_PATH + "\" /opt/r8/monobase/build.sh " + skipCudaArg + " --mini --cache=" + MONOBASE_CACHE_PATH,
	}...), nil
}

func (g *FastGenerator) copyWeights(lines []string, tmpDir string) ([]string, error) {
	weights, err := FindWeights(g.Dir, tmpDir)
	if err != nil {
		return nil, err
	}

	if len(weights) == 0 {
		return lines, nil
	}

	for _, weight := range weights {
		lines = append(lines, "COPY --link \""+weight.Path+"\" \""+filepath.Join(FUSE_RPC_WEIGHTS_PATH, weight.Digest+"\""))
	}

	return lines, nil
}

func (g *FastGenerator) install(lines []string) ([]string, error) {
	// Copy over source
	commands := []string{
		"mkdir -p /src && cp -r /srctmp /src && rm -rf /src/.cog",
	}
	mounts := []string{
		"--mount=type=bind,ro,source=.,target=/srctmp",
	}

	// Install apt packages
	packages := g.Config.Build.SystemPackages
	if len(packages) > 0 {
		mounts = append(mounts, "--mount=type=cache,target=/var/cache/apt,id=apt-cache")
		aptCommand := "apt-get update && apt-get install -qqy "
		aptCommand += strings.Join(packages, " ")
		aptCommand += " && rm -rf /var/lib/apt/lists/*"
		commands = append(commands, aptCommand)
	}

	// Install python packages
	packages, err := g.pythonPackages()
	if err != nil {
		return nil, err
	}
	if len(packages) > 0 {
		mounts = append(mounts, "--mount=type=cache,target=/root/.cache,id=pip-cache")
		pipCommand := "uv pip install "
		pipCommand += strings.Join(packages, " ")
		commands = append(commands, pipCommand)
	}

	// Check that we have no run commands
	if len(g.Config.Build.Run) > 0 {
		return nil, fmt.Errorf("Use of run commands is disallowed in fast push.")
	}

	return append(lines, []string{
		"RUN " + strings.Join(mounts, " ") + " " + strings.Join(commands, " && "),
	}...), nil
}

func (g *FastGenerator) pythonPackages() ([]string, error) {
	packages := g.Config.Build.PythonPackages

	fh, err := os.Open(path.Join(g.Dir, g.Config.Build.PythonRequirements))
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(fh)
	for scanner.Scan() {
		packages = append(packages, scanner.Text())
	}

	return packages, nil
}

func (g *FastGenerator) entrypoint(lines []string) ([]string, error) {
	return append(lines, []string{
		"ENTRYPOINT [\"/usr/bin/tini\", \"--\", \"/opt/r8/monobase/exec.sh\", \"bash\", \"-l\"]",
		"CMD [\"python\", \"-m\", \"cog.server.http\"]",
	}...), nil
}
