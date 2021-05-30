package model

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/replicate/cog/pkg/util/console"
)

// TODO(andreas): support conda packages
// TODO(andreas): support dockerfiles
// TODO(andreas): custom cpu/gpu installs
// TODO(andreas): validate python_requirements
// TODO(andreas): suggest valid torchvision versions (e.g. if the user wants to use 0.8.0, suggest 0.8.1)

type Environment struct {
	PythonVersion        string   `json:"python_version" yaml:"python_version"`
	PythonRequirements   string   `json:"python_requirements" yaml:"python_requirements"`
	PythonExtraIndexURLs []string `json:"python_extra_index_urls" yaml:"python_extra_index_urls"`
	PythonFindLinks      []string `json:"python_find_links" yaml:"python_find_links"`
	PythonPackages       []string `json:"python_packages" yaml:"python_packages"`
	SystemPackages       []string `json:"system_packages" yaml:"system_packages"`
	PreInstall           []string `json:"pre_install" yaml:"pre_install"`
	Architectures        []string `json:"architectures" yaml:"architectures"`
	CUDA                 string   `json:"cuda" yaml:"cuda"`
	CuDNN                string   `json:"cudnn" yaml:"cudnn"`
	BuildRequiresGPU     bool     `json:"build_requires_gpu" yaml:"build_requires_gpu"`
}

type Example struct {
	Input  map[string]string `json:"input" yaml:"input"`
	Output string            `json:"output" yaml:"output"`
}

type Config struct {
	Environment *Environment `json:"environment" yaml:"environment"`
	Model       string       `json:"model" yaml:"model"`
	Examples    []*Example   `json:"examples" yaml:"examples"`
	Workdir     string       `json:"workdir" yaml:"workdir"`
}

func DefaultConfig() *Config {
	return &Config{
		Environment: &Environment{
			PythonVersion: "3.8",
			Architectures: []string{"cpu", "gpu"},
		},
	}
}

func ConfigFromYAML(contents []byte) (*Config, error) {
	config := DefaultConfig()
	if err := yaml.Unmarshal(contents, config); err != nil {
		return nil, fmt.Errorf("Failed to parse config yaml: %w", err)
	}
	return config, nil
}

func (c *Config) HasGPU() bool {
	for _, arch := range c.Environment.Architectures {
		if arch == "gpu" {
			return true
		}
	}
	return false
}

func (c *Config) HasCPU() bool {
	for _, arch := range c.Environment.Architectures {
		if arch == "cpu" {
			return true
		}
	}
	return false
}

func (c *Config) CUDABaseImageTag() (string, error) {
	return CUDABaseImageFor(c.Environment.CUDA, c.Environment.CuDNN)
}

func (c *Config) cudasFromTorch() (torchVersion string, torchCUDAs []string, err error) {
	if version, ok := c.pythonPackageVersion("torch"); ok {
		cudas, err := cudasFromTorch(version)
		if err != nil {
			return "", nil, err
		}
		return version, cudas, nil
	}
	return "", nil, nil
}

func (c *Config) cudaFromTF() (tfVersion string, tfCUDA string, tfCuDNN string, err error) {
	if version, ok := c.pythonPackageVersion("tensorflow"); ok {
		cuda, cudnn, err := cudaFromTF(version)
		if err != nil {
			return "", "", "", err
		}
		return version, cuda, cudnn, nil
	}
	return "", "", "", nil
}

func (c *Config) pythonPackageVersion(name string) (version string, ok bool) {
	for _, pkg := range c.Environment.PythonPackages {
		pkgName, version, err := splitPythonPackage(pkg)
		if err != nil {
			// this should be caught by validation earlier
			console.Warnf("Python package %s is malformed.", pkg)
			return "", false
		}
		if pkgName == name {
			return version, true
		}
	}
	return "", false
}

func (c *Config) ValidateAndCompleteConfig() error {
	// TODO(andreas): return all errors at once, rather than
	// whack-a-mole one at a time with errs := []error{}, etc.

	// TODO(andreas): validate that torch/torchvision/torchaudio are compatible
	// TODO(andreas): warn if user specifies tensorflow-gpu instead of tensorflow
	// TODO(andreas): use pypi api to validate that all python versions exist

	if c.Model != "" {
		if len(strings.Split(c.Model, ".py:")) != 2 {
			return fmt.Errorf("'model' in cog.yaml must be in the form 'model.py:ModelClass")
		}
		if strings.Contains(c.Model, "/") {
			return fmt.Errorf("'model' in cog.yaml cannot be in a subdirectory. It needs to be in the same directory as cog.yaml, in the form 'model.py:ModelClass")
		}
	}

	if err := c.validatePythonPackagesHaveVersions(); err != nil {
		return err
	}

	if c.HasGPU() {
		if err := c.validateAndCompleteCUDA(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) PythonPackagesForArch(arch string, goos string, goarch string) (packages []string, indexURLs []string, err error) {
	packages = []string{}
	indexURLSet := map[string]bool{}
	for _, pkg := range c.Environment.PythonPackages {
		archPkg, indexURL, err := c.pythonPackageForArch(pkg, arch, goos, goarch)
		if err != nil {
			return nil, nil, err
		}
		packages = append(packages, archPkg)
		if indexURL != "" {
			indexURLSet[indexURL] = true
		}
	}
	indexURLs = []string{}
	for indexURL := range indexURLSet {
		indexURLs = append(indexURLs, indexURL)
	}
	return packages, indexURLs, nil
}

func (c *Config) pythonPackageForArch(pkg string, arch string, goos string, goarch string) (actualPackage string, indexURL string, err error) {
	name, version, err := splitPythonPackage(pkg)
	if err != nil {
		return "", "", err
	}
	if name == "tensorflow" {
		if arch == "cpu" {
			name, version, err = tfCPUPackage(version)
			if err != nil {
				return "", "", err
			}
		} else {
			name, version, err = tfGPUPackage(version, c.Environment.CUDA)
			if err != nil {
				return "", "", err
			}
		}
	} else if name == "torch" {
		if arch == "cpu" {
			name, version, indexURL, err = torchCPUPackage(version, goos, goarch)
			if err != nil {
				return "", "", err
			}
		} else {
			name, version, indexURL, err = torchGPUPackage(version, c.Environment.CUDA)
			if err != nil {
				return "", "", err
			}
		}
	} else if name == "torchvision" {
		if arch == "cpu" {
			name, version, indexURL, err = torchvisionCPUPackage(version, goos, goarch)
			if err != nil {
				return "", "", err
			}
		} else {
			name, version, indexURL, err = torchvisionGPUPackage(version, c.Environment.CUDA)
			if err != nil {
				return "", "", err
			}
		}
	}
	pkgWithVersion := name
	if version != "" {
		pkgWithVersion += "==" + version
	}
	return pkgWithVersion, indexURL, nil
}

func (c *Config) validateAndCompleteCUDA() error {
	if c.Environment.CUDA != "" && c.Environment.CuDNN != "" {
		compatibleCuDNNs := compatibleCuDNNsForCUDA(c.Environment.CUDA)
		if !sliceContains(compatibleCuDNNs, c.Environment.CuDNN) {
			return fmt.Errorf(`The specified CUDA version %s is not compatible with CuDNN %s.
Compatible CuDNN versions are: %s`, c.Environment.CUDA, c.Environment.CuDNN, strings.Join(compatibleCuDNNs, ","))
		}
	}

	torchVersion, torchCUDAs, err := c.cudasFromTorch()
	if err != nil {
		return err
	}
	tfVersion, tfCUDA, tfCuDNN, err := c.cudaFromTF()
	if err != nil {
		return err
	}
	// The pre-compiled TensorFlow binaries requires specific CUDA/CuDNN versions to be
	// installed, but Torch bundles their own CUDA/CuDNN libraries.

	if tfVersion != "" {
		if c.Environment.CUDA == "" {
			console.Infof("Setting CUDA to version %s from Tensorflow version", tfCUDA)
			c.Environment.CUDA = tfCUDA
		} else if tfCUDA != c.Environment.CUDA {
			return fmt.Errorf(`The specified CUDA version %s is not compatible with tensorflow==%s.
Compatible CUDA version is: %s`,
				c.Environment.CUDA, tfVersion, tfCUDA)
		}
		if c.Environment.CuDNN == "" {
			console.Infof("Setting CuDNN to version %s from Tensorflow version", tfCuDNN)
			c.Environment.CuDNN = tfCuDNN
		} else if tfCuDNN != c.Environment.CuDNN {
			return fmt.Errorf(`The specified cuDNN version %s is not compatible with tensorflow==%s.
Compatible cuDNN version is: %s`,
				c.Environment.CuDNN, tfVersion, tfCuDNN)
		}
	} else if torchVersion != "" {
		if c.Environment.CUDA == "" {
			c.Environment.CUDA = latestCUDAFrom(torchCUDAs)
			console.Infof("Setting CUDA to version %s from Torch version", c.Environment.CUDA)
		}
		if c.Environment.CuDNN == "" {
			c.Environment.CuDNN = latestCuDNNForCUDA(c.Environment.CUDA)
			console.Infof("Setting CuDNN to version %s", c.Environment.CUDA)
		}
	} else {
		if c.Environment.CUDA == "" {
			c.Environment.CUDA = defaultCUDA()
			console.Infof("Setting CUDA to version %s", c.Environment.CUDA)
		}
		if c.Environment.CuDNN == "" {
			c.Environment.CuDNN = latestCuDNNForCUDA(c.Environment.CUDA)
			console.Infof("Setting CuDNN to version %s", c.Environment.CUDA)
		}
	}

	return nil
}

func (c *Config) validatePythonPackagesHaveVersions() error {
	packagesWithoutVersions := []string{}
	for _, pkg := range c.Environment.PythonPackages {
		_, _, err := splitPythonPackage(pkg)
		if err != nil {
			packagesWithoutVersions = append(packagesWithoutVersions, pkg)
		}
	}
	if len(packagesWithoutVersions) > 0 {
		return fmt.Errorf(`All Python packages must have pinned versions, e.g. mypkg==1.0.0.
The following packages are missing pinned versions: %s`, strings.Join(packagesWithoutVersions, ","))
	}
	return nil
}

func splitPythonPackage(pkg string) (name string, version string, err error) {
	if strings.HasPrefix(pkg, "git+") {
		return pkg, "", nil
	}

	if !strings.Contains(pkg, "==") {
		return "", "", fmt.Errorf("Package %s is not in the format 'name==version'", pkg)
	}
	parts := strings.Split(pkg, "==")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("Package %s is not in the format 'name==version'", pkg)
	}
	return parts[0], parts[1], nil
}

func sliceContains(slice []string, s string) bool {
	for _, el := range slice {
		if el == s {
			return true
		}
	}
	return false
}
