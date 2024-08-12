package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/slices"
	"github.com/replicate/cog/pkg/util/version"
)

var (
	BuildSourceEpochTimestamp int64 = -1
	BuildXCachePath           string
)

// TODO(andreas): support conda packages
// TODO(andreas): support dockerfiles
// TODO(andreas): custom cpu/gpu installs
// TODO(andreas): suggest valid torchvision versions (e.g. if the user wants to use 0.8.0, suggest 0.8.1)

const (
	MinimumMajorPythonVersion int = 3
	MinimumMinorPythonVersion int = 8
	MinimumMajorCudaVersion   int = 11
)

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
}

func DefaultConfig() *Config {
	return &Config{
		Build: &Build{
			GPU:           false,
			PythonVersion: "3.12",
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

func (c *Config) TorchVersion() (string, bool) {
	return c.pythonPackageVersion("torch")
}

func (c *Config) TorchvisionVersion() (string, bool) {
	return c.pythonPackageVersion("torchvision")
}

func (c *Config) TensorFlowVersion() (string, bool) {
	return c.pythonPackageVersion("tensorflow")
}

func (c *Config) cudasFromTorch() (torchVersion string, torchCUDAs []string, err error) {
	if version, ok := c.TorchVersion(); ok {
		cudas, err := cudasFromTorch(version)
		if err != nil {
			return "", nil, err
		}
		return version, cudas, nil
	}
	return "", nil, nil
}

func (c *Config) cudaFromTF() (tfVersion string, tfCUDA string, tfCuDNN string, err error) {
	if version, ok := c.TensorFlowVersion(); ok {
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
		pkgName, version, _, _, err := splitPinnedPythonRequirement(pkg)
		if err != nil {
			// package is not in package==version format
			continue
		}
		if pkgName == name {
			return version, true
		}
	}
	return "", false
}

func splitPythonVersion(version string) (major int, minor int, err error) {
	version = strings.TrimSpace(version)
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("missing minor version in %s", version)
	}
	majorStr, minorStr := parts[0], parts[1]
	major, err = strconv.Atoi(majorStr)
	if err != nil {
		return 0, 0, err
	}
	minor, err = strconv.Atoi(minorStr)
	if err != nil {
		return 0, 0, err
	}
	return major, minor, nil
}

func ValidateModelPythonVersion(version string) error {
	// we check for minimum supported here
	major, minor, err := splitPythonVersion(version)
	if err != nil {
		return fmt.Errorf("invalid Python version format: %w", err)
	}
	if major < MinimumMajorPythonVersion || (major >= MinimumMajorPythonVersion &&
		minor < MinimumMinorPythonVersion) {
		return fmt.Errorf("minimum supported Python version is %d.%d. requested %s",
			MinimumMajorPythonVersion, MinimumMinorPythonVersion, version)
	}
	return nil
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
func (c *Config) PythonRequirementsForArch(goos string, goarch string, excludePackages []string) (string, error) {
	packages := []string{}
	findLinksSet := map[string]bool{}
	extraIndexURLSet := map[string]bool{}
	for _, pkg := range c.Build.pythonRequirementsContent {
		if slices.ContainsString(excludePackages, pkg) {
			continue
		}

		archPkg, findLinksList, extraIndexURLs, err := c.pythonPackageForArch(pkg, goos, goarch)
		if err != nil {
			return "", err
		}
		packages = append(packages, archPkg)
		if len(findLinksList) > 0 {
			for _, fl := range findLinksList {
				findLinksSet[fl] = true
			}
		}
		if len(extraIndexURLs) > 0 {
			for _, u := range extraIndexURLs {
				extraIndexURLSet[u] = true
			}
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
func (c *Config) pythonPackageForArch(pkg, goos, goarch string) (actualPackage string, findLinksList []string, extraIndexURLs []string, err error) {
	name, version, findLinksList, extraIndexURLs, err := splitPinnedPythonRequirement(pkg)
	if err != nil {
		// It's not pinned, so just return the line verbatim
		return pkg, []string{}, []string{}, nil
	}
	if len(extraIndexURLs) > 0 {
		return name + "==" + version, findLinksList, extraIndexURLs, nil
	}

	extraIndexURL := ""
	findLinks := ""
	switch name {
	case "tensorflow":
		if c.Build.GPU {
			name, version, err = tfGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", nil, nil, err
			}
		}
		// There is no CPU case for tensorflow because the default package is just the CPU package, so no transformation of version is needed
	case "torch":
		if c.Build.GPU {
			name, version, findLinks, extraIndexURL, err = torchGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", nil, nil, err
			}
		} else {
			name, version, findLinks, extraIndexURL, err = torchCPUPackage(version, goos, goarch)
			if err != nil {
				return "", nil, nil, err
			}
		}
	case "torchvision":
		if c.Build.GPU {
			name, version, findLinks, extraIndexURL, err = torchvisionGPUPackage(version, c.Build.CUDA)
			if err != nil {
				return "", nil, nil, err
			}
		} else {
			name, version, findLinks, extraIndexURL, err = torchvisionCPUPackage(version, goos, goarch)
			if err != nil {
				return "", nil, nil, err
			}
		}
	}
	pkgWithVersion := name
	if version != "" {
		pkgWithVersion += "==" + version
	}
	if extraIndexURL != "" {
		extraIndexURLs = []string{extraIndexURL}
	}
	if findLinks != "" {
		findLinksList = []string{findLinks}
	}
	return pkgWithVersion, findLinksList, extraIndexURLs, nil
}

func ValidateCudaVersion(cudaVersion string) error {
	parts := strings.Split(cudaVersion, ".")
	if len(parts) < 2 {
		return fmt.Errorf("CUDA version %q must include both major and minor versions", cudaVersion)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("Invalid major version in CUDA version %q", cudaVersion)
	}

	if major < MinimumMajorCudaVersion {
		return fmt.Errorf("Minimum supported CUDA version is %d. requested %q", MinimumMajorCudaVersion, cudaVersion)
	}
	return nil
}

func (c *Config) validateAndCompleteCUDA() error {
	if c.Build.CUDA != "" {
		if err := ValidateCudaVersion(c.Build.CUDA); err != nil {
			return err
		}
	}

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

	switch {
	case tfVersion != "":
		switch {
		case c.Build.CUDA == "":
			if tfCuDNN == "" {
				return fmt.Errorf("Cog doesn't know what CUDA version is compatible with tensorflow==%s. You might need to upgrade Cog: https://github.com/replicate/cog#upgrade\n\nIf that doesn't work, you need to set the 'cuda' option in cog.yaml to set what version to use. You might be able to find this out from https://www.tensorflow.org/", tfVersion)
			}
			console.Debugf("Setting CUDA to version %s from Tensorflow version", tfCUDA)
			c.Build.CUDA = tfCUDA
		case tfCUDA == "" || version.EqualMinor(tfCUDA, c.Build.CUDA):
			console.Warnf("Cog doesn't know if CUDA %s is compatible with Tensorflow %s. This might cause CUDA problems.", c.Build.CUDA, tfVersion)
			if tfCUDA != "" {
				console.Warnf("Try %s instead?", tfCUDA)
			}
		}

		switch {
		case c.Build.CuDNN == "" && tfCuDNN != "":
			console.Debugf("Setting CuDNN to version %s from Tensorflow version", tfCuDNN)
			c.Build.CuDNN = tfCuDNN
		case c.Build.CuDNN == "":
			c.Build.CuDNN, err = latestCuDNNForCUDA(c.Build.CUDA)
			if err != nil {
				return err
			}
			console.Debugf("Setting CuDNN to version %s", c.Build.CUDA)
		case tfCuDNN != c.Build.CuDNN:
			console.Warnf("Cog doesn't know if cuDNN %s is compatible with Tensorflow %s. This might cause CUDA problems.", c.Build.CuDNN, tfVersion)
			return fmt.Errorf(`The specified cuDNN version %s is not compatible with tensorflow==%s.
Compatible cuDNN version is: %s`, c.Build.CuDNN, tfVersion, tfCuDNN)
		}
	case torchVersion != "":
		switch {
		case c.Build.CUDA == "":
			if len(torchCUDAs) == 0 {
				return fmt.Errorf("Cog doesn't know what CUDA version is compatible with torch==%s. You might need to upgrade Cog: https://github.com/replicate/cog#upgrade\n\nIf that doesn't work, you need to set the 'cuda' option in cog.yaml to set what version to use. You might be able to find this out from https://pytorch.org/", torchVersion)
			}
			c.Build.CUDA = latestCUDAFrom(torchCUDAs)
			console.Debugf("Setting CUDA to version %s from Torch version", c.Build.CUDA)
		case len(slices.FilterString(torchCUDAs, func(torchCUDA string) bool { return version.EqualMinor(torchCUDA, c.Build.CUDA) })) == 0:
			// TODO: can we suggest a CUDA version known to be compatible?
			console.Warnf("Cog doesn't know if CUDA %s is compatible with PyTorch %s. This might cause CUDA problems.", c.Build.CUDA, torchVersion)
			if len(torchCUDAs) > 0 {
				console.Warnf("Try %s instead?", torchCUDAs[len(torchCUDAs)-1])
			}
		}

		if c.Build.CuDNN == "" {
			c.Build.CuDNN, err = latestCuDNNForCUDA(c.Build.CUDA)
			if err != nil {
				return err
			}
			console.Debugf("Setting CuDNN to version %s", c.Build.CUDA)
		}
	default:
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

// splitPythonPackage returns the name, version, findLinks, and extraIndexURLs from a requirements.txt line
// in the form name==version [--find-links=<findLink>] [-f <findLink>] [--extra-index-url=<extraIndexURL>]
func splitPinnedPythonRequirement(requirement string) (name string, version string, findLinks []string, extraIndexURLs []string, err error) {
	pinnedPackageRe := regexp.MustCompile(`(?:([a-zA-Z0-9\-_]+)==([^ ]+)|--find-links=([^\s]+)|-f\s+([^\s]+)|--extra-index-url=([^\s]+))`)

	matches := pinnedPackageRe.FindAllStringSubmatch(requirement, -1)
	if matches == nil {
		return "", "", nil, nil, fmt.Errorf("Package %s is not in the expected format", requirement)
	}

	nameFound := false
	versionFound := false

	for _, match := range matches {
		if match[1] != "" {
			name = match[1]
			nameFound = true
		}

		if match[2] != "" {
			version = match[2]
			versionFound = true
		}

		if match[3] != "" {
			findLinks = append(findLinks, match[3])
		}

		if match[4] != "" {
			findLinks = append(findLinks, match[4])
		}

		if match[5] != "" {
			extraIndexURLs = append(extraIndexURLs, match[5])
		}
	}

	if !nameFound || !versionFound {
		return "", "", nil, nil, fmt.Errorf("Package name or version is missing in %s", requirement)
	}

	return name, version, findLinks, extraIndexURLs, nil
}

func sliceContains(slice []string, s string) bool {
	for _, el := range slice {
		if el == s {
			return true
		}
	}
	return false
}
