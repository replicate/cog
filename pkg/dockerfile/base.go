package dockerfile

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util/version"
)

const BaseImageRegistry = "r8.im"
const MinimumCUDAVersion = "11.6"
const MinimumPythonVersion = "3.8"
const MinimumTorchVersion = "1.13.1"

var (
	baseImageSystemPackages = []string{
		"build-essential",
		"cmake",
		"curl",
		"ffmpeg",
		"g++",
		"gcc",
		"git",
		"libavcodec-dev",
		"libcairo2-dev",
		"libfontconfig1",
		"libgirepository1.0-dev",
		"libgl1",
		"libgl1-mesa-glx",
		"libglib2.0-0",
		"libsm6",
		"libsndfile1",
		"libssl-dev",
		"libunistring-dev",
		"libxext6",
		"libxrender1",
		"python3-opencv",
		"sox",
		"unzip",
		"wget",
		"zip",
	}
)

type CUDAVersion struct {
	Version string `json:"versions"`
}

type PyTorchVersion struct {
	Version string `json:"version"`
}

type PythonVersion struct {
	Version string           `json:"version"`
	PyTorch []PyTorchVersion `json:"pytorch"`
	CUDA    []CUDAVersion    `json:"cuda"`
}

type AvailableBaseImageConfigurations struct {
	PythonVersions []PythonVersion `json:"python_versions"`
}

type BaseImageConfiguration struct {
	CUDAVersion   string `json:"cuda_version" yaml:"cuda_version"`
	PythonVersion string `json:"python_version" yaml:"python_version"`
	TorchVersion  string `json:"torch_version" yaml:"torch_version"`
}

type BaseImageGenerator struct {
	cudaVersion   string
	pythonVersion string
	torchVersion  string
}

func (b BaseImageConfiguration) MarshalJSON() ([]byte, error) {
	type Alias BaseImageConfiguration
	type BaseImageConfigWithImageName struct {
		Alias
		ImageName string `json:"image_name,omitempty" yaml:"image_name,omitempty"`
		Tag       string `json:"image_tag,omitempty" yaml:"image_tag,omitempty"`
	}

	rawName := BaseImageName(b.CUDAVersion, b.PythonVersion, b.TorchVersion)
	rawName = strings.TrimPrefix(rawName, BaseImageRegistry+"/")
	split := strings.Split(rawName, ":")
	if len(split) != 2 {
		return nil, fmt.Errorf("invalid base image name and tag: %s", rawName)
	}
	imageName, tag := split[0], split[1]

	alias := &BaseImageConfigWithImageName{
		Alias:     Alias(b),
		ImageName: imageName,
		Tag:       tag,
	}
	return json.Marshal(alias)
}

// BaseImageConfigurations returns a list of CUDA/Python/Torch versions
// with patch versions stripped out. Each version is greater or equal to
// MinimumCUDAVersion/MinimumPythonVersion/MinimumTorchVersion.
func BaseImageConfigurations() []BaseImageConfiguration {
	configs := []BaseImageConfiguration{}

	// Assuming that the Torch versions cover all Python and CUDA versions to avoid
	// having to hard-code a list of Python versions here.
	pythonVersionsSet := make(map[string]bool)
	cudaVersionsSet := make(map[string]bool)

	// Torch configs
	for _, compat := range config.TorchMinorCompatibilityMatrix {
		for _, python := range compat.Pythons {

			// Only support fast cold boots for Torch with CUDA.
			// Torch without CUDA is a rarely used edge case.
			if compat.CUDA == nil {
				continue
			}

			cuda := *compat.CUDA
			torch := version.StripPatch(compat.Torch)
			conf := BaseImageConfiguration{
				CUDAVersion:   cuda,
				PythonVersion: python,
				TorchVersion:  torch,
			}

			if (version.GreaterOrEqual(cuda, MinimumCUDAVersion)) &&
				version.GreaterOrEqual(python, MinimumPythonVersion) &&
				version.GreaterOrEqual(compat.Torch, MinimumTorchVersion) {
				configs = append(configs, conf)
				pythonVersionsSet[python] = true
				cudaVersionsSet[cuda] = true
			}
		}
	}

	// Python and CUDA-only configs
	for python := range pythonVersionsSet {
		for cuda := range cudaVersionsSet {
			configs = append(configs, BaseImageConfiguration{
				CUDAVersion:   cuda,
				PythonVersion: python,
			})
		}
	}

	// Python-only configs
	for python := range pythonVersionsSet {
		configs = append(configs, BaseImageConfiguration{
			PythonVersion: python,
		})
	}

	return configs
}

func NewBaseImageGenerator(cudaVersion string, pythonVersion string, torchVersion string) (*BaseImageGenerator, error) {
	if BaseImageConfigurationExists(cudaVersion, pythonVersion, torchVersion) {
		return &BaseImageGenerator{cudaVersion, pythonVersion, torchVersion}, nil
	}
	printNone := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}
	return nil, fmt.Errorf("unsupported base image configuration: CUDA: %s / Python: %s / Torch: %s", printNone(cudaVersion), printNone(pythonVersion), printNone(torchVersion))
}

func (g *BaseImageGenerator) GenerateDockerfile() (string, error) {
	conf, err := g.makeConfig()
	if err != nil {
		return "", err
	}

	generator, err := NewGenerator(conf, "")
	if err != nil {
		return "", err
	}

	dockerfile, err := generator.generateInitialSteps()
	if err != nil {
		return "", err
	}

	return dockerfile, nil
}

func (g *BaseImageGenerator) makeConfig() (*config.Config, error) {
	conf := &config.Config{
		Build: &config.Build{
			GPU:            g.cudaVersion != "",
			PythonVersion:  g.pythonVersion,
			PythonPackages: g.pythonPackages(),
			Run:            g.runStatements(),
			SystemPackages: baseImageSystemPackages,
			CUDA:           g.cudaVersion,
		},
	}
	if err := conf.ValidateAndComplete(""); err != nil {
		return nil, err
	}
	return conf, nil
}

func (g *BaseImageGenerator) pythonPackages() []string {
	if g.torchVersion != "" {
		return []string{"torch==" + g.torchVersion}
	}
	return []string{}
}

func (g *BaseImageGenerator) runStatements() []config.RunItem {
	return []config.RunItem{}
}

func BaseImageName(cudaVersion string, pythonVersion string, torchVersion string) string {
	components := []string{}
	if cudaVersion != "" {
		components = append(components, "cuda"+version.StripPatch(cudaVersion))
	}
	if pythonVersion != "" {
		components = append(components, "python"+version.StripPatch(pythonVersion))
	}
	if torchVersion != "" {
		components = append(components, "torch"+version.StripPatch(torchVersion))
	}

	tag := strings.Join(components, "-")
	if tag == "" {
		tag = "latest"
	}

	return BaseImageRegistry + "/cog-base:" + tag
}

func BaseImageConfigurationExists(cudaVersion, pythonVersion, torchVersion string) bool {
	for _, conf := range BaseImageConfigurations() {
		// Check CUDA version compatibility
		if !isVersionCompatible(conf.CUDAVersion, cudaVersion) {
			continue
		}

		// Check Python version compatibility
		if !isVersionCompatible(conf.PythonVersion, pythonVersion) {
			continue
		}

		// Check Torch version compatibility
		if !isVersionCompatible(conf.TorchVersion, torchVersion) {
			continue
		}

		return true
	}
	return false
}

func isVersionCompatible(confVersion, requestedVersion string) bool {
	if confVersion == "" || requestedVersion == "" {
		return confVersion == requestedVersion
	}
	return version.Matches(confVersion, requestedVersion)
}
