package dockerfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights"
)

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
	cogPath, err := g.copyCog(tmpDir)
	if err != nil {
		return nil, err
	}
	relativeTmpDir, err := filepath.Rel(g.Dir, tmpDir)
	if err != nil {
		return nil, err
	}
	skipCudaArg := "--skip-cuda"
	cudaVersion := "12.4"
	cudnnVersion := "9"
	if g.Config.Build.GPU {
		skipCudaArg = ""
		cudaVersion = g.Config.Build.CUDA
		cudnnVersion = g.Config.Build.CuDNN
	}
	torchVersion, err := g.Config.TorchVersion()
	if err != nil {
		return nil, err
	}

	return append(lines, []string{
		"FROM monobase:latest",
		"ENV R8_COG_VERSION=\"file:///buildtmp/" + filepath.Base(cogPath) + "\"",
		"ENV R8_CUDA_VERSION=" + cudaVersion,
		"ENV R8_CUDNN_VERSION=" + cudnnVersion,
		"ENV R8_CUDA_PREFIX=https://monobase.replicate.delivery/cuda",
		"ENV R8_CUDNN_PREFIX=https://monobase.replicate.delivery/cudnn",
		"ENV R8_PYTHON_VERSION=" + g.Config.Build.PythonVersion,
		"ENV R8_TORCH_VERSION=" + torchVersion,
		"RUN --mount=type=bind,source=\"" + relativeTmpDir + "\",target=/buildtmp /opt/r8/monobase/build.sh " + skipCudaArg + " --mini",
	}...), nil
}
