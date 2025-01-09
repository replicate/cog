package dockerfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights"
)

const FUSE_RPC_WEIGHTS_PATH = "/srv/r8/fuse-rpc/weights"

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

	lines, err = g.copyWeights(lines)
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
		"FROM monobase:latest",
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
		"RUN --mount=type=bind,source=\"" + relativeTmpDir + "\",target=/buildtmp /opt/r8/monobase/build.sh " + skipCudaArg + " --mini",
	}...), nil
}

func (g *FastGenerator) copyWeights(lines []string) ([]string, error) {
	weights, err := FindWeights(g.Dir)
	if err != nil {
		return nil, err
	}

	if len(weights) == 0 {
		return lines, nil
	}

	commands := []string{}
	for sha256, file := range weights {
		rel_path, err := filepath.Rel(g.Dir, file)
		if err != nil {
			return nil, err
		}
		commands = append(commands, "cp /src/"+rel_path+" "+filepath.Join(FUSE_RPC_WEIGHTS_PATH, sha256))
	}

	return append(lines, []string{
		"RUN --mount=type=bind,ro,source=.,target=/src mkdir -p " + FUSE_RPC_WEIGHTS_PATH + " && " + strings.Join(commands, " && "),
	}...), nil
}
