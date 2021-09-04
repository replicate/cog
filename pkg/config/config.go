package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/slices"
)

// TODO(andreas): support conda packages
// TODO(andreas): support dockerfiles
// TODO(andreas): custom cpu/gpu installs
// TODO(andreas): validate python_requirements
// TODO(andreas): suggest valid torchvision versions (e.g. if the user wants to use 0.8.0, suggest 0.8.1)

type Build struct {
	GPU                  bool     `json:"gpu,omitempty" yaml:"gpu"`
	PythonVersion        string   `json:"python_version,omitempty" yaml:"python_version"`
	PythonRequirements   string   `json:"python_requirements,omitempty" yaml:"python_requirements"`
	PythonExtraIndexURLs []string `json:"python_extra_index_urls,omitempty" yaml:"python_extra_index_urls"`
	PythonFindLinks      []string `json:"python_find_links,omitempty" yaml:"python_find_links"`
	PythonPackages       []string `json:"python_packages,omitempty" yaml:"python_packages"`
	Run                  []string `json:"run,omitempty" yaml:"run"`
	SystemPackages       []string `json:"system_packages,omitempty" yaml:"system_packages"`
	PreInstall           []string `json:"pre_install,omitempty" yaml:"pre_install"` // Deprecated, but included for backwards compatibility
	CUDA                 string   `json:"cuda,omitempty" yaml:"cuda"`
	CuDNN                string   `json:"cudnn,omitempty" yaml:"cudnn"`
}

type Example struct {
	Input  map[string]string `json:"input" yaml:"input"`
	Output string            `json:"output" yaml:"output"`
}

type Config struct {
	Build   *Build `json:"build" yaml:"build"`
	Image   string `json:"image,omitempty" yaml:"image"`
	Predict string `json:"predict,omitempty" yaml:"predict"`
}

func DefaultConfig() *Config {
	return &Config{
		Build: &Build{
			GPU:           false,
			PythonVersion: "3.8",
		},
	}
}

func ConfigFromYAML(contents []byte) (*Config, error) {
	config := DefaultConfig()
	if err := yaml.Unmarshal(contents, config); err != nil {
		return nil, fmt.Errorf("Failed to parse config yaml: %w", err)
	}
	// Everything assumes Build is not nil
	if config.Build == nil {
		config.Build = DefaultConfig().Build
	}
	return config, nil
}

func (c *Config) CUDABaseImageTag() (string, error) {
	return CUDABaseImageFor(c.Build.CUDA, c.Build.CuDNN)
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
	for _, pkg := range c.Build.PythonPackages {
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

	if c.Predict != "" {
		if len(strings.Split(c.Predict, ".py:")) != 2 {
			return fmt.Errorf("'predict' in cog.yaml must be in the form 'predict.py:PredictorClass")
		}
		if strings.Contains(c.Predict, "/") {
			return fmt.Errorf("'predict' in cog.yaml cannot be in a subdirectory. It needs to be in the same directory as cog.yaml, in the form 'predict.py:PredictorClass")
		}
	}

	if err := c.validatePythonPackagesHaveVersions(); err != nil {
		return err
	}

	if c.Build.GPU {
		if err := c.validateAndCompleteCUDA(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) PythonPackagesForArch(goos string, goarch string) (packages []string, indexURLs []string, err error) {
	packages = []string{}
	indexURLSet := map[string]bool{}
	for _, pkg := range c.Build.PythonPackages {
		archPkg, indexURL, err := c.pythonPackageForArch(pkg, goos, goarch)
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

func (c *Config) pythonPackageForArch(pkg string, goos string, goarch string) (actualPackage string, indexURL string, err error) {
	name, version, err := splitPythonPackage(pkg)
	if err != nil {
		return "", "", err
	}
	if name == "tensorflow" {
		if c.Build.GPU {
			name, version, err = tfGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", "", err
			}
		}
		// There is no CPU case for tensorflow because the default package is just the CPU package, so no transformation of version is needed
	} else if name == "torch" {
		if c.Build.GPU {
			name, version, indexURL, err = torchGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", "", err
			}
		} else {
			name, version, indexURL, err = torchCPUPackage(version, goos, goarch)
			if err != nil {
				return "", "", err
			}
		}
	} else if name == "torchvision" {
		if c.Build.GPU {
			name, version, indexURL, err = torchvisionGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", "", err
			}
		} else {
			name, version, indexURL, err = torchvisionCPUPackage(version, goos, goarch)
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
	if c.Build.CUDA != "" && c.Build.CuDNN != "" {
		compatibleCuDNNs := compatibleCuDNNsForCUDA(c.Build.CUDA)
		if !sliceContains(compatibleCuDNNs, c.Build.CuDNN) {
			return fmt.Errorf(`The specified CUDA version %s is not compatible with CuDNN %s.
Compatible CuDNN versions are: %s`, c.Build.CUDA, c.Build.CuDNN, strings.Join(compatibleCuDNNs, ","))
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
		if c.Build.CUDA == "" {
			console.Debugf("Setting CUDA to version %s from Tensorflow version", tfCUDA)
			c.Build.CUDA = tfCUDA
		} else if tfCUDA != c.Build.CUDA {
			return fmt.Errorf(`The specified CUDA version %s is not compatible with tensorflow==%s.
Compatible CUDA version is: %s`,
				c.Build.CUDA, tfVersion, tfCUDA)
		}
		if c.Build.CuDNN == "" {
			console.Debugf("Setting CuDNN to version %s from Tensorflow version", tfCuDNN)
			c.Build.CuDNN = tfCuDNN
		} else if tfCuDNN != c.Build.CuDNN {
			return fmt.Errorf(`The specified cuDNN version %s is not compatible with tensorflow==%s.
Compatible cuDNN version is: %s`,
				c.Build.CuDNN, tfVersion, tfCuDNN)
		}
	} else if torchVersion != "" {
		if c.Build.CUDA == "" {
			if len(torchCUDAs) == 0 {
				return fmt.Errorf("Cog couldn't automatically determine a CUDA version for torch==%s. You need to set the 'cuda' option in cog.yaml to set what version to use. You might be able to find this out from https://pytorch.org/", torchVersion)
			}
			c.Build.CUDA = latestCUDAFrom(torchCUDAs)
			console.Debugf("Setting CUDA to version %s from Torch version", c.Build.CUDA)
		} else if !slices.ContainsString(torchCUDAs, c.Build.CUDA) {
			// TODO: can we suggest a CUDA version known to be compatible?
			console.Warnf("Cog doesn't know if CUDA %s is compatible with PyTorch %s. This might cause CUDA problems.", c.Build.CUDA, torchVersion)
		}

		if c.Build.CuDNN == "" {
			c.Build.CuDNN = latestCuDNNForCUDA(c.Build.CUDA)
			console.Debugf("Setting CuDNN to version %s", c.Build.CUDA)
		}
	} else {
		if c.Build.CUDA == "" {
			c.Build.CUDA = defaultCUDA()
			console.Debugf("Setting CUDA to version %s", c.Build.CUDA)
		}
		if c.Build.CuDNN == "" {
			c.Build.CuDNN = latestCuDNNForCUDA(c.Build.CUDA)
			console.Debugf("Setting CuDNN to version %s", c.Build.CUDA)
		}
	}

	return nil
}

func (c *Config) validatePythonPackagesHaveVersions() error {
	packagesWithoutVersions := []string{}
	for _, pkg := range c.Build.PythonPackages {
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
