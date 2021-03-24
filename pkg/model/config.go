package model

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
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
	Architectures        []string `json:"architectures" yaml:"architectures"`
	CUDA                 string   `json:"cuda" yaml:"cuda"`
	CuDNN                string   `json:"cudnn" yaml:"cudnn"`
}

type Example struct {
	Input  map[string]string `json:"input" yaml:"input"`
	Output string            `json:"output" yaml:"output"`
}

type Config struct {
	Environment *Environment `json:"environment" yaml:"environment"`
	Model       string       `json:"model" yaml:"model"`
	Examples    []*Example   `json:"examples" yaml:"examples"`
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
			log.Warnf("Python package %s is malformed.", pkg)
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

func (c *Config) PythonPackagesForArch(arch string) (packages []string, indexURLs []string, err error) {
	packages = []string{}
	indexURLSet := map[string]bool{}
	for _, pkg := range c.Environment.PythonPackages {
		archPkg, indexURL, err := c.pythonPackageForArch(pkg, arch)
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

func (c *Config) pythonPackageForArch(pkg string, arch string) (actualPackage string, indexURL string, err error) {
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
			name, version, indexURL, err = torchCPUPackage(version)
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
			name, version, indexURL, err = torchvisionCPUPackage(version)
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
	var cudas []string
	var cuDNN string

	if torchVersion != "" && tfVersion != "" {
		// both torch and tensorflow are used
		if sliceContains(torchCUDAs, tfCUDA) {
			cudas = []string{tfCUDA}
		} else {
			return fmt.Errorf(`Incompatible CUDA versions for the specified tensorflow and torch versions.
tensorflow==%s works with CUDA %s; torch==%s works with CUDA %s"`,
				tfVersion, tfCUDA,
				torchVersion, strings.Join(torchCUDAs, ","))
		}
		if c.Environment.CUDA != "" && !sliceContains(cudas, c.Environment.CUDA) {
			return fmt.Errorf(`The specified CUDA version %s is not compatible with tensorflow==%s and torch==%s.
Compatible CUDA versions are: %s`,
				c.Environment.CUDA, tfVersion, torchVersion, strings.Join(cudas, ","))
		}
	} else if torchVersion != "" {
		// only torch is set
		cudas = torchCUDAs
		if c.Environment.CUDA != "" && !sliceContains(cudas, c.Environment.CUDA) {
			return fmt.Errorf(`The specified CUDA version %s is not compatible with torch==%s.
Compatible CUDA versions are: %s`,
				c.Environment.CUDA, torchVersion, strings.Join(cudas, ","))
		}
	} else if tfVersion != "" {
		// only tensorflow is set
		cudas = []string{tfCUDA}
		if c.Environment.CUDA != "" && !sliceContains(cudas, c.Environment.CUDA) {
			return fmt.Errorf(`The specified CUDA version %s is not compatible with tensorflow==%s.
Compatible CUDA versions are: %s`,
				c.Environment.CUDA, tfVersion, strings.Join(cudas, ","))
		}
		if c.Environment.CuDNN != "" && c.Environment.CuDNN != tfCuDNN {
			return fmt.Errorf(`The specified cuDNN version %s is not compatible with tensorflow==%s.
Compatible cuDNN version is: %s`,
				c.Environment.CuDNN, tfVersion, tfCuDNN)
		}
		cuDNN = tfCuDNN
	}

	if c.Environment.CUDA == "" {
		if len(cudas) == 0 {
			c.Environment.CUDA = defaultCUDA()
			log.Infof("Setting CUDA to version %s", c.Environment.CUDA)
		} else {
			c.Environment.CUDA = latestCUDAFrom(cudas)
			log.Infof("Setting CUDA to version %s from torch/tensorflow version", c.Environment.CUDA)
		}
	}
	if c.Environment.CuDNN == "" {
		if cuDNN == "" {
			c.Environment.CuDNN = latestCuDNNForCUDA(c.Environment.CUDA)
			log.Infof("Setting CuDNN to version %s", c.Environment.CuDNN)
		} else {
			c.Environment.CuDNN = cuDNN
			log.Infof("Setting CuDNN to version %s from torch/tensorflow version", c.Environment.CuDNN)
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
		return name, "", nil
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

func setIntersect(a []string, b []string) []string {
	aMap := map[string]bool{}
	bMap := map[string]bool{}
	for _, s := range a {
		aMap[s] = true
	}
	for _, s := range b {
		bMap[s] = true
	}
	ret := []string{}
	for key := range aMap {
		if _, ok := bMap[key]; ok {
			ret = append(ret, key)
		}
	}
	return ret
}

func sliceContains(slice []string, s string) bool {
	for _, el := range slice {
		if el == s {
			return true
		}
	}
	return false
}
