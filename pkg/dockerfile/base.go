package dockerfile

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/replicate/cog/pkg/config"
)

const BaseImageRegistry = "r8.im"

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
	CudaVersion   string `json:"cuda_version" yaml:"cuda_version"`
	PythonVersion string `json:"python_version" yaml:"python_version"`
	TorchVersion  string `json:"torch_version" yaml:"torch_version"`
}

type BaseImageGenerator struct {
	cudaVersion   string
	pythonVersion string
	torchVersion  string
}

func ToBaseImageConfigurations(configurations []AvailableBaseImageConfigurations) []BaseImageConfiguration {
	var baseImageConfigs []BaseImageConfiguration

	for _, conf := range configurations {
		for _, pythonVersion := range conf.PythonVersions {
			for _, torchVersion := range pythonVersion.PyTorch {
				for _, cudaVersion := range pythonVersion.CUDA {
					baseImageConfigs = append(baseImageConfigs, BaseImageConfiguration{
						CudaVersion:   cudaVersion.Version,
						PythonVersion: pythonVersion.Version,
						TorchVersion:  torchVersion.Version,
					})
				}
			}
		}
	}

	return baseImageConfigs
}

func (b BaseImageConfiguration) MarshalJSON() ([]byte, error) {
	type Alias BaseImageConfiguration
	type BaseImageConfigWithImageName struct {
		Alias
		ImageName string `json:"image_name,omitempty" yaml:"image_name,omitempty"`
		Tag       string `json:"image_tag,omitempty" yaml:"image_tag,omitempty"`
	}

	rawName := BaseImageName(b.CudaVersion, b.PythonVersion, b.TorchVersion)
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

func BaseImageConfigurations() []AvailableBaseImageConfigurations {
	return []AvailableBaseImageConfigurations{
		{
			PythonVersions: []PythonVersion{
				{
					Version: "3.11",
					PyTorch: []PyTorchVersion{
						{Version: "2.0.0"},
						{Version: "2.0.1"},
						{Version: "2.1.0"},
						{Version: "2.1.1"},
						{Version: "2.2.0"},
					},
					CUDA: []CUDAVersion{
						{Version: "11.6.2"},
						{Version: "11.8"},
					},
				},
				{
					Version: "3.10",
					PyTorch: []PyTorchVersion{
						{Version: "1.12.1"},
					},
					CUDA: []CUDAVersion{
						{Version: "11.2"},
						{Version: "11.3"},
						{Version: "11.6"},
						{Version: "11.6.2"},
						{Version: "11.7"},
						{Version: "11.7.1"},
						{Version: "11.8"},
						{Version: "11.8.0"},
						{Version: "12.1"},
					},
				},
				{
					Version: "3.9",
					PyTorch: []PyTorchVersion{
						{Version: "1.11.0"},
						{Version: "1.13.0"},
						{Version: "2.0.0"},
						{Version: "2.0.1"},
					},
					CUDA: []CUDAVersion{
						{Version: "11.2"},
						{Version: "11.3"},
						{Version: "11.3.1"},
						{Version: "11.6"},
						{Version: "11.6.2"},
						{Version: "11.7"},
						{Version: "11.7.1"},
						{Version: "11.8"},
						{Version: "11.8.0"},
					},
				},
				{
					Version: "3.8",
					PyTorch: []PyTorchVersion{
						{Version: "1.7.1"},
						{Version: "1.8.0"},
						{Version: "1.9.0"},
						{Version: "1.11.0"},
						{Version: "1.12.1"},
						{Version: "2.0.0"},
						{Version: "2.0.1"},
						{Version: "1.13.0"},
						{Version: "1.13.1"},
					},
					CUDA: []CUDAVersion{
						{Version: "11.0.3"},
						{Version: "11.1"},
						{Version: "11.1.1"},
						{Version: "11.2"},
						{Version: "11.3"},
						{Version: "11.3.1"},
						{Version: "11.4"},
						{Version: "11.6"},
						{Version: "11.6.2"},
						{Version: "11.7"},
						{Version: "11.7.1"},
						{Version: "11.8"},
						{Version: "11.8.0"},
					},
				},
				{
					Version: "3.7",
					CUDA: []CUDAVersion{
						{Version: "11.8"},
					},
				},
			},
		},
	}
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
	tag := "python" + pythonVersion
	if cudaVersion != "" {
		tag = "cuda" + cudaVersion + "-" + tag
	}
	if torchVersion != "" {
		tag += "-torch" + torchVersion
	}
	return BaseImageRegistry + "/cog-base:" + tag
}

func BaseImageConfigurationExists(cudaVersion, pythonVersion, torchVersion string) bool {
	for _, conf := range BaseImageConfigurations() {
		for _, pyVer := range conf.PythonVersions {
			if pyVer.Version == pythonVersion {
				// If torchVersion is empty, it means we are checking for Python and CUDA only
				if torchVersion == "" {
					for _, cudaVer := range pyVer.CUDA {
						if cudaVer.Version == cudaVersion {
							return true
						}
					}
				} else {
					for _, torchVer := range pyVer.PyTorch {
						if torchVer.Version == torchVersion {
							// If CUDA version is empty, it means we are checking for Python and PyTorch only
							if cudaVersion == "" {
								return true
							}
							for _, cudaVer := range pyVer.CUDA {
								if cudaVer.Version == cudaVersion {
									return true
								}
							}
						}
					}
				}
			}
		}
	}
	return false
}
