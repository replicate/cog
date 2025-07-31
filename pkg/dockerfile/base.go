package dockerfile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/version"
)

const MinimumCUDAVersion = "11.6"
const MinimumPythonVersion = "3.8"
const MinimumTorchVersion = "1.13.1"
const CogBaseImageName = "cog-base"

var (
	baseImageSystemPackages = []string{
		"build-essential",
		"cmake",
		"curl",
		"ffmpeg",
		"findutils",
		"g++",
		"gcc",
		"git",
		"libavcodec-dev",
		"libcairo2-dev",
		"libfontconfig1",
		"libgirepository1.0-dev",
		"libgl1",
		"libglx-mesa0",
		"libglib2.0-0",
		"libopencv-dev",
		"libsm6",
		"libsndfile1",
		"libssl-dev",
		"libunistring-dev",
		"libxext6",
		"libxrender1",
		"sox",
		"unzip",
		"wget",
		"zip",
		"zstd",
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
	command       command.Command
	client        registry.Client
}

func (b BaseImageConfiguration) MarshalJSON() ([]byte, error) {
	type Alias BaseImageConfiguration
	type BaseImageConfigWithImageName struct {
		Alias
		ImageName string `json:"image_name,omitempty" yaml:"image_name,omitempty"`
		Tag       string `json:"image_tag,omitempty" yaml:"image_tag,omitempty"`
	}

	rawName := BaseImageName(b.CUDAVersion, b.PythonVersion, b.TorchVersion)
	rawName = strings.TrimPrefix(rawName, global.ReplicateRegistryHost+"/")
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
func BaseImageConfigurations() []BaseImageConfiguration {
	configs := []BaseImageConfiguration{}

	// Assuming that the Torch versions cover all Python and CUDA versions to avoid
	// having to hard-code a list of Python versions here.
	pythonVersionsSet := make(map[string]bool)
	cudaVersionsSet := make(map[string]bool)

	// Torch configs
	for _, compat := range config.TorchCompatibilityMatrix {
		for _, python := range compat.Pythons {
			if !version.GreaterOrEqual(python, MinimumPythonVersion) || !version.GreaterOrEqual(compat.Torch, MinimumTorchVersion) {
				continue
			}

			if compat.CUDA == nil {
				configs = append(configs, BaseImageConfiguration{
					PythonVersion: python,
					TorchVersion:  compat.Torch,
				})
			} else {
				cuda := *compat.CUDA
				torch := compat.Torch
				conf := BaseImageConfiguration{
					CUDAVersion:   cuda,
					PythonVersion: python,
					TorchVersion:  torch,
				}
				if version.GreaterOrEqual(cuda, MinimumCUDAVersion) {
					configs = append(configs, conf)
					pythonVersionsSet[python] = true
					cudaVersionsSet[cuda] = true
				}
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

func NewBaseImageGenerator(ctx context.Context, client registry.Client, cudaVersion string, pythonVersion string, torchVersion string, command command.Command, generate bool) (*BaseImageGenerator, error) {
	valid, cudaVersion, pythonVersion, torchVersion, err := BaseImageConfigurationExists(ctx, client, cudaVersion, pythonVersion, torchVersion, generate)
	if err != nil {
		return nil, err
	}
	if valid {
		return &BaseImageGenerator{cudaVersion, pythonVersion, torchVersion, command, client}, nil
	}
	printNone := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}
	return nil, fmt.Errorf("unsupported base image configuration: CUDA: %s / Python: %s / Torch: %s", printNone(cudaVersion), printNone(pythonVersion), printNone(torchVersion))
}

func (g *BaseImageGenerator) GenerateDockerfile(ctx context.Context) (string, error) {
	conf, err := g.makeConfig()
	if err != nil {
		return "", err
	}

	generator, err := NewGenerator(conf, "", false, g.command, true, g.client, false)
	if err != nil {
		return "", err
	}
	useCogBaseImage := false
	generator.SetUseCogBaseImagePtr(&useCogBaseImage)

	dockerfile, err := generator.GenerateInitialSteps(ctx)
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
		pkgs := []string{
			"torch==" + g.torchVersion,
			"opencv-python==4.12.0.88",
		}

		// Find torchvision compatibility.
		for _, compat := range config.TorchCompatibilityMatrix {
			if len(compat.Torchvision) == 0 {
				continue
			}
			if !version.Matches(g.torchVersion, compat.TorchVersion()) {
				continue
			}

			pkgs = append(pkgs, "torchvision=="+compat.Torchvision)
			break
		}

		// Find torchaudio compatibility.
		for _, compat := range config.TorchCompatibilityMatrix {
			if len(compat.Torchaudio) == 0 {
				continue
			}
			if !version.Matches(g.torchVersion, compat.TorchVersion()) {
				continue
			}

			pkgs = append(pkgs, "torchaudio=="+compat.Torchaudio)
			break
		}

		return pkgs
	}
	return []string{}
}

func (g *BaseImageGenerator) runStatements() []config.RunItem {
	return []config.RunItem{}
}

func baseImageComponentNormalisation(cudaVersion string, pythonVersion string, torchVersion string) (string, string, string) {
	compatibleTorchVersion := ""
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

		if compatibleTorchVersion == "" || version.Greater(conf.TorchVersion, compatibleTorchVersion) {
			compatibleTorchVersion = version.StripModifier(conf.TorchVersion)
		}
	}

	return cudaVersion, pythonVersion, compatibleTorchVersion
}

func BaseImageName(cudaVersion string, pythonVersion string, torchVersion string) string {
	cudaVersion, pythonVersion, torchVersion = baseImageComponentNormalisation(cudaVersion, pythonVersion, torchVersion)

	components := []string{}
	if cudaVersion != "" {
		components = append(components, "cuda"+version.StripPatch(cudaVersion))
	}
	if pythonVersion != "" {
		components = append(components, "python"+version.StripPatch(pythonVersion))
	}
	if torchVersion != "" {
		components = append(components, "torch"+version.StripModifier(torchVersion))
	}

	tag := strings.Join(components, "-")
	if tag == "" {
		tag = "latest"
	}

	return global.ReplicateRegistryHost + "/" + CogBaseImageName + ":" + tag
}

func BaseImageConfigurationExists(ctx context.Context, client registry.Client, cudaVersion, pythonVersion, torchVersion string, generate bool) (bool, string, string, string, error) {
	cudaVersion, pythonVersion, torchVersion = baseImageComponentNormalisation(cudaVersion, pythonVersion, torchVersion)

	valid := false
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

		valid = true
	}

	var err error
	if valid && !generate {
		valid, err = client.Exists(ctx, BaseImageName(cudaVersion, pythonVersion, torchVersion))
	}

	return valid, cudaVersion, pythonVersion, torchVersion, err
}

func isVersionCompatible(confVersion, requestedVersion string) bool {
	if confVersion == "" || requestedVersion == "" {
		return confVersion == requestedVersion
	}
	return version.Matches(requestedVersion, confVersion)
}
