package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Manifest represents the top-level structure of manifest.yaml
type Manifest struct {
	Defaults Defaults `yaml:"defaults"`
	Models   []Model  `yaml:"models"`
}

// Defaults holds default versions
type Defaults struct {
	SDKVersion string `yaml:"sdk_version"`
	CogVersion string `yaml:"cog_version"`
}

// Model represents a single model definition
type Model struct {
	Name             string            `yaml:"name"`
	Repo             string            `yaml:"repo"`
	Path             string            `yaml:"path"`
	GPU              bool              `yaml:"gpu"`
	Timeout          int               `yaml:"timeout"`
	RequiresEnv      []string          `yaml:"requires_env"`
	Env              map[string]string `yaml:"env"`
	SDKVersion       string            `yaml:"sdk_version"`
	CogYAMLOverrides map[string]any    `yaml:"cog_yaml_overrides"`
	Setup            []string          `yaml:"setup"`
	Tests            []TestCase        `yaml:"tests"`
	TrainTests       []TestCase        `yaml:"train_tests"`
}

// TestCase represents a single test case
type TestCase struct {
	Description string         `yaml:"description"`
	Inputs      map[string]any `yaml:"inputs"`
	Expect      Expectation    `yaml:"expect"`
}

// Expectation represents expected test output
type Expectation struct {
	Type    string         `yaml:"type"`
	Value   any            `yaml:"value"`
	Pattern string         `yaml:"pattern"`
	Mime    string         `yaml:"mime"`
	Match   map[string]any `yaml:"match"`
	Keys    []string       `yaml:"keys"`
}

// Load loads a manifest from the given path or auto-detects it
func Load(explicitPath string) (*Manifest, string, error) {
	manifestPath, err := resolvePath(explicitPath)
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("reading manifest: %w", err)
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, "", fmt.Errorf("parsing manifest: %w", err)
	}

	// Apply default timeout
	for i := range manifest.Models {
		if manifest.Models[i].Timeout == 0 {
			manifest.Models[i].Timeout = 300
		}
	}

	return &manifest, manifestPath, nil
}

// resolvePath resolves the manifest path using multiple strategies
func resolvePath(explicitPath string) (string, error) {
	// 1. Explicit path
	if explicitPath != "" {
		absPath, err := filepath.Abs(explicitPath)
		if err != nil {
			return "", fmt.Errorf("resolving manifest path: %w", err)
		}
		return absPath, nil
	}

	// 2. Relative to working directory
	cwd, err := os.Getwd()
	if err == nil {
		candidates := []string{
			filepath.Join(cwd, "manifest.yaml"),
			filepath.Join(cwd, "tools", "test-harness", "manifest.yaml"),
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				absPath, err := filepath.Abs(candidate)
				if err == nil {
					return absPath, nil
				}
			}
		}
	}

	// 3. Auto-detect from source location
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		sourceDir := filepath.Dir(filename)
		candidate := filepath.Join(sourceDir, "..", "..", "manifest.yaml")
		if _, err := os.Stat(candidate); err == nil {
			absPath, err := filepath.Abs(candidate)
			if err == nil {
				return absPath, nil
			}
		}
	}

	return "", fmt.Errorf("manifest not found: specify --manifest or run from project root")
}

// GetModel returns a model by name
func (m *Manifest) GetModel(name string) *Model {
	for i := range m.Models {
		if m.Models[i].Name == name {
			return &m.Models[i]
		}
	}
	return nil
}

// FilterModels returns models matching the given criteria
func (m *Manifest) FilterModels(names []string, noGPU, gpuOnly bool) []Model {
	var filtered []Model
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}

	for _, model := range m.Models {
		// Filter by name
		if len(nameSet) > 0 && !nameSet[model.Name] {
			continue
		}

		// Filter by GPU
		if noGPU && model.GPU {
			continue
		}
		if gpuOnly && !model.GPU {
			continue
		}

		filtered = append(filtered, model)
	}

	return filtered
}
