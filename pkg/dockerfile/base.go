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

func BaseImageConfigurations() []BaseImageConfiguration {
	// TODO(andreas): Support every combination for recent
	// versions, and a subset of combinations for older but
	// popular combinations.
	return []BaseImageConfiguration{
		{"", "3.10", ""},
		{"", "3.10", "1.12.1"},
		{"", "3.11", ""},
		{"", "3.8", ""},
		{"", "3.9", ""},
		{"11.0.3", "3.8", "1.7.1"},
		{"11.1", "3.8", "1.8.0"},
		{"11.1.1", "3.8", "1.8.0"},
		{"11.1.1", "3.8", "1.9.0"},
		{"11.2", "3.10", ""},
		{"11.2", "3.8", ""},
		{"11.2", "3.9", ""},
		{"11.2.2", "3.8", "2.0.2"},
		{"11.3", "3.10", ""},
		{"11.3", "3.10", "1.12.0"},
		{"11.3", "3.8", ""},
		{"11.3", "3.8", "0.1.5"},
		{"11.3", "3.8", "1.11.0"},
		{"11.3", "3.8", "1.12.1"},
		{"11.3.1", "3.8", "1.11.0"},
		{"11.3.1", "3.9", "1.11.0"},
		{"11.4", "3.8", "1.9.1"},
		{"11.4", "3.10", "1.13.0"},
		{"11.6", "3.10", ""},
		{"11.6", "3.10", "1.13.0"},
		{"11.6", "3.10", "1.13.1"},
		{"11.6", "3.10", "2.0.0"},
		{"11.6", "3.8", ""},
		{"11.6", "3.8", "2.0.0"},
		{"11.6", "3.9", ""},
		{"11.6", "3.9", "1.13.0"},
		{"11.6", "3.9", "2.0.0"},
		{"11.6.2", "3.10", ""},
		{"11.6.2", "3.10", "1.12.1"},
		{"11.6.2", "3.10", "2.0.1"},
		{"11.6.2", "3.11", "2.0.0"},
		{"11.6.2", "3.8", "1.12.1"},
		{"11.6.2", "3.9", "2.0.1"},
		{"11.7", "3.10", ""},
		{"11.7", "3.10", "1.13.0"},
		{"11.7", "3.10", "1.13.1"},
		{"11.7", "3.10", "2.0.0"},
		{"11.7", "3.10", "2.0.1"},
		{"11.7", "3.8", ""},
		{"11.7", "3.8", "1.13.1"},
		{"11.7", "3.8", "2.0.0"},
		{"11.7", "3.8", "2.0.1"},
		{"11.7", "3.9", ""},
		{"11.7", "3.9", "2.0.1"},
		{"11.7.1", "3.10", ""},
		{"11.7.1", "3.10", "1.13.0"},
		{"11.7.1", "3.8", "1.13.0"},
		{"11.7.1", "3.9", "1.13.0"},
		{"11.7.1", "3.9", "1.13.1"},
		{"11.8", "3.10", ""},
		{"11.8", "3.10", "2.0.0"},
		{"11.8", "3.10", "2.0.1"},
		{"11.8", "3.10", "2.1.0"},
		{"11.8", "3.10", "2.20.0"},
		{"11.8", "3.11", ""},
		{"11.8", "3.11", "2.0.1"},
		{"11.8", "3.11", "2.1.0"},
		{"11.8", "3.11", "2.1.1"},
		{"11.8", "3.11", "2.20.0"},
		{"11.8", "3.7", ""},
		{"11.8", "3.8", ""},
		{"11.8", "3.8", "2.0.1"},
		{"11.8", "3.9", ""},
		{"11.8", "3.9", "2.0.0"},
		{"11.8", "3.9", "2.0.1"},
		{"11.8", "3.9", "2.20.0"},
		{"11.8.0", "3.10", "2.0.0"},
		{"11.8.0", "3.10", "2.0.1"},
		{"11.8.0", "3.11", "2.0.1"},
		{"11.8.0", "3.8", "2.0.0"},
		{"11.8.0", "3.8", "2.0.1"},
		{"11.8.0", "3.9", "1.13.1"},
		{"11.8.0", "3.9", "2.0.1"},
		{"12.1", "3.10", ""},
		{"12.1", "3.10", "2.1.0"},
		{"12.1", "3.10", "2.1.1"},
		{"12.1", "3.10", "2.1.2"},
		{"12.1", "3.11", ""},
		{"12.1", "3.11", "2.1.0"},
		{"12.1", "3.11", "2.1.1"},
		{"12.1", "3.9", ""},
		{"12.1", "3.9", "2.1.0"},
		{"12.1.1", "3.11", "2.1.1"},
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
	return nil, fmt.Errorf("Unsupported base image configuration: CUDA: %s / Python: %s / Torch: %s", printNone(cudaVersion), printNone(pythonVersion), printNone(torchVersion))
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
		if conf.CudaVersion == cudaVersion && conf.PythonVersion == pythonVersion && conf.TorchVersion == torchVersion {
			return true
		}
	}
	return false
}
