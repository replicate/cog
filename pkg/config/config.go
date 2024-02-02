package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/slices"
)

// TODO(andreas): support conda packages
// TODO(andreas): support dockerfiles
// TODO(andreas): custom cpu/gpu installs
// TODO(andreas): suggest valid torchvision versions (e.g. if the user wants to use 0.8.0, suggest 0.8.1)

type RunItem struct {
	Command string `json:"command,omitempty" yaml:"command"`
	Mounts  []struct {
		Type   string `json:"type,omitempty" yaml:"type"`
		ID     string `json:"id,omitempty" yaml:"id"`
		Target string `json:"target,omitempty" yaml:"target"`
	} `json:"mounts,omitempty" yaml:"mounts"`
}

type Build struct {
	GPU                bool      `json:"gpu,omitempty" yaml:"gpu"`
	PythonVersion      string    `json:"python_version,omitempty" yaml:"python_version"`
	PythonRequirements string    `json:"python_requirements,omitempty" yaml:"python_requirements"`
	PythonPackages     []string  `json:"python_packages,omitempty" yaml:"python_packages"` // Deprecated, but included for backwards compatibility
	Run                []RunItem `json:"run,omitempty" yaml:"run"`
	SystemPackages     []string  `json:"system_packages,omitempty" yaml:"system_packages"`
	PreInstall         []string  `json:"pre_install,omitempty" yaml:"pre_install"` // Deprecated, but included for backwards compatibility
	CUDA               string    `json:"cuda,omitempty" yaml:"cuda"`
	CuDNN              string    `json:"cudnn,omitempty" yaml:"cudnn"`

	pythonRequirementsContent []string
}

type Example struct {
	Input  map[string]string `json:"input" yaml:"input"`
	Output string            `json:"output" yaml:"output"`
}

type Config struct {
	Build   *Build `json:"build" yaml:"build"`
	Image   string `json:"image,omitempty" yaml:"image"`
	Predict string `json:"predict,omitempty" yaml:"predict"`
	Train   string `json:"train,omitempty" yaml:"train"`
    Concurrency int `json:"concurrency,omitempty" yaml:"concurrency"`
}

func DefaultConfig() *Config {
	return &Config{
		Build: &Build{
			GPU:           false,
			PythonVersion: "3.8",
		},
	}
}

func (r *RunItem) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var commandOrMap interface{}
	if err := unmarshal(&commandOrMap); err != nil {
		return err
	}

	switch v := commandOrMap.(type) {
	case string:
		r.Command = v
	case map[interface{}]interface{}:
		var data []byte
		var err error

		if data, err = yaml.Marshal(v); err != nil {
			return err
		}

		aux := struct {
			Command string `yaml:"command"`
			Mounts  []struct {
				Type   string `yaml:"type"`
				ID     string `yaml:"id"`
				Target string `yaml:"target"`
			} `yaml:"mounts,omitempty"`
		}{}

		if err := yaml.Unmarshal(data, &aux); err != nil {
			return err
		}

		*r = RunItem(aux)
	default:
		return fmt.Errorf("unexpected type %T for RunItem", v)
	}

	return nil
}

func (r *RunItem) UnmarshalJSON(data []byte) error {
	var commandOrMap interface{}
	if err := json.Unmarshal(data, &commandOrMap); err != nil {
		return err
	}

	switch v := commandOrMap.(type) {
	case string:
		r.Command = v
	case map[string]interface{}:
		aux := struct {
			Command string `json:"command"`
			Mounts  []struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				Target string `json:"target"`
			} `json:"mounts,omitempty"`
		}{}

		jsonData, err := json.Marshal(v)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(jsonData, &aux); err != nil {
			return err
		}

		*r = RunItem(aux)
	default:
		return fmt.Errorf("unexpected type %T for RunItem", v)
	}

	return nil
}

func FromYAML(contents []byte) (*Config, error) {
	config := DefaultConfig()
	if err := yaml.Unmarshal(contents, config); err != nil {
		return nil, fmt.Errorf("Failed to parse config yaml: %w", err)
	}
	// Everything assumes Build is not nil
	if len(contents) != 0 && config.Build != nil {
		err := Validate(string(contents), "")
		if err != nil {
			return nil, err
		}
	} else {
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
	for _, pkg := range c.Build.pythonRequirementsContent {
		pkgName, version, err := splitPinnedPythonRequirement(pkg)
		if err != nil {
			return "", false
		}
		if pkgName == name {
			return version, true
		}
	}
	return "", false
}

func (c *Config) ValidateAndComplete(projectDir string) error {
	// TODO(andreas): validate that torch/torchvision/torchaudio are compatible
	// TODO(andreas): warn if user specifies tensorflow-gpu instead of tensorflow
	// TODO(andreas): use pypi api to validate that all python versions exist

	errs := []error{}

	err := ValidateConfig(c, "")
	if err != nil {
		errs = append(errs, err)
	}

	if c.Predict != "" {
		if len(strings.Split(c.Predict, ".py:")) != 2 {
			errs = append(errs, fmt.Errorf("'predict' in cog.yaml must be in the form 'predict.py:Predictor"))
		}
	}

	if len(c.Build.PythonPackages) > 0 && c.Build.PythonRequirements != "" {
		errs = append(errs, fmt.Errorf("Only one of python_packages or python_requirements can be set in your cog.yaml, not both"))
	}

	// Load python_requirements into memory to simplify reading it multiple times
	if c.Build.PythonRequirements != "" {
		fh, err := os.Open(path.Join(projectDir, c.Build.PythonRequirements))
		if err != nil {
			errs = append(errs, fmt.Errorf("Failed to open python_requirements file: %w", err))
		}
		// Use scanner to handle CRLF endings
		scanner := bufio.NewScanner(fh)
		for scanner.Scan() {
			c.Build.pythonRequirementsContent = append(c.Build.pythonRequirementsContent, scanner.Text())
		}
	}

	// Backwards compatibility
	if len(c.Build.PythonPackages) > 0 {
		c.Build.pythonRequirementsContent = c.Build.PythonPackages
	}

	if c.Build.GPU {
		if err := c.validateAndCompleteCUDA(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// PythonRequirementsForArch returns a requirements.txt file with all the GPU packages resolved for given OS and architecture.
func (c *Config) PythonRequirementsForArch(goos string, goarch string) (string, error) {
	packages := []string{}
	findLinksSet := map[string]bool{}
	extraIndexURLSet := map[string]bool{}
	for _, pkg := range c.Build.pythonRequirementsContent {
		archPkg, findLinks, extraIndexURL, err := c.pythonPackageForArch(pkg, goos, goarch)
		if err != nil {
			return "", err
		}
		packages = append(packages, archPkg)
		if findLinks != "" {
			findLinksSet[findLinks] = true
		}
		if extraIndexURL != "" {
			extraIndexURLSet[extraIndexURL] = true
		}
	}

	// Create final requirements.txt output
	// Put index URLs first
	lines := []string{}
	for findLinks := range findLinksSet {
		lines = append(lines, "--find-links "+findLinks)
	}
	for extraIndexURL := range extraIndexURLSet {
		lines = append(lines, "--extra-index-url "+extraIndexURL)
	}

	// Then, everything else
	lines = append(lines, packages...)

	return strings.Join(lines, "\n"), nil
}

// pythonPackageForArch takes a package==version line and
// returns a package==version and index URL resolved to the correct GPU package for the given OS and architecture
func (c *Config) pythonPackageForArch(pkg, goos, goarch string) (actualPackage, findLinks, extraIndexURL string, err error) {
	name, version, err := splitPinnedPythonRequirement(pkg)
	if err != nil {
		// It's not pinned, so just return the line verbatim
		return pkg, "", "", nil
	}
	if name == "tensorflow" {
		if c.Build.GPU {
			name, version, err = tfGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", "", "", err
			}
		}
		// There is no CPU case for tensorflow because the default package is just the CPU package, so no transformation of version is needed
	} else if name == "torch" {
		if c.Build.GPU {
			name, version, findLinks, extraIndexURL, err = torchGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", "", "", err
			}
		} else {
			name, version, findLinks, extraIndexURL, err = torchCPUPackage(version, goos, goarch)
			if err != nil {
				return "", "", "", err
			}
		}
	} else if name == "torchvision" {
		if c.Build.GPU {
			name, version, findLinks, extraIndexURL, err = torchvisionGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", "", "", err
			}
		} else {
			name, version, findLinks, extraIndexURL, err = torchvisionCPUPackage(version, goos, goarch)
			if err != nil {
				return "", "", "", err
			}
		}
	}
	pkgWithVersion := name
	if version != "" {
		pkgWithVersion += "==" + version
	}
	return pkgWithVersion, findLinks, extraIndexURL, nil
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
			if tfCuDNN == "" {
				return fmt.Errorf("Cog doesn't know what CUDA version is compatible with tensorflow==%s. You might need to upgrade Cog: https://github.com/replicate/cog#upgrade\n\nIf that doesn't work, you need to set the 'cuda' option in cog.yaml to set what version to use. You might be able to find this out from https://www.tensorflow.org/", tfVersion)
			}
			console.Debugf("Setting CUDA to version %s from Tensorflow version", tfCUDA)
			c.Build.CUDA = tfCUDA
		} else if tfCUDA != c.Build.CUDA {
			// TODO: can we suggest a CUDA version known to be compatible?
			console.Warnf("Cog doesn't know if CUDA %s is compatible with Tensorflow %s. This might cause CUDA problems.", c.Build.CUDA, tfVersion)
		}
		if c.Build.CuDNN == "" && tfCuDNN != "" {
			console.Debugf("Setting CuDNN to version %s from Tensorflow version", tfCuDNN)
			c.Build.CuDNN = tfCuDNN
		} else if c.Build.CuDNN == "" {
			c.Build.CuDNN, err = latestCuDNNForCUDA(c.Build.CUDA)
			if err != nil {
				return err
			}
			console.Debugf("Setting CuDNN to version %s", c.Build.CUDA)
		} else if tfCuDNN != c.Build.CuDNN {
			console.Warnf("Cog doesn't know if cuDNN %s is compatible with Tensorflow %s. This might cause CUDA problems.", c.Build.CuDNN, tfVersion)
			return fmt.Errorf(`The specified cuDNN version %s is not compatible with tensorflow==%s.
Compatible cuDNN version is: %s`,
				c.Build.CuDNN, tfVersion, tfCuDNN)
		}
	} else if torchVersion != "" {
		if c.Build.CUDA == "" {
			if len(torchCUDAs) == 0 {
				return fmt.Errorf("Cog doesn't know what CUDA version is compatible with torch==%s. You might need to upgrade Cog: https://github.com/replicate/cog#upgrade\n\nIf that doesn't work, you need to set the 'cuda' option in cog.yaml to set what version to use. You might be able to find this out from https://pytorch.org/", torchVersion)
			}
			c.Build.CUDA = latestCUDAFrom(torchCUDAs)
			c.Build.CUDA, err = resolveMinorToPatch(c.Build.CUDA)
			if err != nil {
				return err
			}
			console.Debugf("Setting CUDA to version %s from Torch version", c.Build.CUDA)
		} else if !slices.ContainsString(torchCUDAs, c.Build.CUDA) {
			// TODO: can we suggest a CUDA version known to be compatible?
			console.Warnf("Cog doesn't know if CUDA %s is compatible with PyTorch %s. This might cause CUDA problems.", c.Build.CUDA, torchVersion)
		}

		if c.Build.CuDNN == "" {
			c.Build.CuDNN, err = latestCuDNNForCUDA(c.Build.CUDA)
			if err != nil {
				return err
			}
			console.Debugf("Setting CuDNN to version %s", c.Build.CUDA)
		}
	} else {
		if c.Build.CUDA == "" {
			c.Build.CUDA = defaultCUDA()
			console.Debugf("Setting CUDA to version %s", c.Build.CUDA)
		}
		if c.Build.CuDNN == "" {
			c.Build.CuDNN, err = latestCuDNNForCUDA(c.Build.CUDA)
			if err != nil {
				return err
			}
			console.Debugf("Setting CuDNN to version %s", c.Build.CUDA)
		}
	}

	return nil
}

// splitPythonPackage returns the name and version from a requirements.txt line in the form name==version
func splitPinnedPythonRequirement(requirement string) (name string, version string, err error) {
	pinnedPackageRe := regexp.MustCompile(`^([a-zA-Z0-9\-_]+)==([\d\.]+)$`)

	match := pinnedPackageRe.FindStringSubmatch(requirement)
	if match == nil {
		return "", "", fmt.Errorf("Package %s is not in the format 'name==version'", requirement)
	}
	return match[1], match[2], nil
}

func sliceContains(slice []string, s string) bool {
	for _, el := range slice {
		if el == s {
			return true
		}
	}
	return false
}
