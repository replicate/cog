package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"

	"github.com/replicate/cog/pkg/util/files"
)

// parse reads and parses YAML content from an io.Reader into a configFile.
// This only does YAML parsing - no validation or defaults.
// Returns ParseError if the content cannot be read or parsed.
func parse(r io.Reader) (*configFile, error) {
	contents, err := io.ReadAll(r)
	if err != nil {
		return nil, &ParseError{Err: err}
	}

	return parseBytes(contents)
}

// parseFile reads and parses a cog.yaml file into a configFile.
// This only does YAML parsing - no validation or defaults.
// Returns ParseError if the file cannot be read or parsed.
func parseFile(filename string) (*configFile, error) {
	exists, err := files.Exists(filename)
	if err != nil {
		return nil, &ParseError{Filename: filename, Err: err}
	}

	if !exists {
		return nil, &ParseError{
			Filename: filename,
			Err:      fmt.Errorf("%s does not exist in %s", filepath.Base(filename), filepath.Dir(filename)),
		}
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, &ParseError{Filename: filename, Err: err}
	}
	defer f.Close()

	cfg, err := parse(f)
	if err != nil {
		// Add filename context to the error
		if parseErr, ok := err.(*ParseError); ok {
			parseErr.Filename = filename
			return nil, parseErr
		}
		return nil, &ParseError{Filename: filename, Err: err}
	}

	return cfg, nil
}

// parseBytes parses YAML content into a configFile.
func parseBytes(contents []byte) (*configFile, error) {
	cfg := &configFile{}

	if len(contents) == 0 {
		// Empty file is valid, returns empty config
		return cfg, nil
	}

	if err := yaml.Unmarshal(contents, cfg); err != nil {
		return nil, &ParseError{
			Err: fmt.Errorf("invalid YAML: %w", err),
		}
	}

	return cfg, nil
}

// FromYAML parses YAML content into an uncompleted Config.
// This is a convenience function primarily for testing.
// Callers should call Complete() on the returned config to resolve CUDA versions etc.
// For production code, use Load() which handles validation and completion.
//
// Note: This function skips validation since it has no project directory context.
// The Complete() method will validate requirements files exist when called.
func FromYAML(contents []byte) (*Config, error) {
	cfgFile, err := parseBytes(contents)
	if err != nil {
		return nil, err
	}

	// Convert to Config struct without completion or validation
	// The caller should call Complete() with the appropriate project dir
	return configFileToConfig(cfgFile)
}

// configFileToConfig converts a ConfigFile to a Config without running completion logic.
// This is the minimal conversion used by FromYAML for test compatibility.
func configFileToConfig(cfg *configFile) (*Config, error) {
	config := &Config{
		Build: &Build{},
	}

	if cfg.Build != nil {
		if cfg.Build.GPU != nil {
			config.Build.GPU = *cfg.Build.GPU
		}
		if cfg.Build.PythonVersion != nil {
			config.Build.PythonVersion = *cfg.Build.PythonVersion
		} else {
			config.Build.PythonVersion = DefaultPythonVersion
		}
		if cfg.Build.PythonRequirements != nil {
			config.Build.PythonRequirements = *cfg.Build.PythonRequirements
		}
		config.Build.PythonPackages = cfg.Build.PythonPackages
		config.Build.SystemPackages = cfg.Build.SystemPackages
		config.Build.PreInstall = cfg.Build.PreInstall
		if cfg.Build.CUDA != nil {
			config.Build.CUDA = *cfg.Build.CUDA
		}
		if cfg.Build.CuDNN != nil {
			config.Build.CuDNN = *cfg.Build.CuDNN
		}

		// Convert Run items
		config.Build.Run = make([]RunItem, len(cfg.Build.Run))
		for i, runFile := range cfg.Build.Run {
			config.Build.Run[i] = RunItem{
				Command: runFile.Command,
			}
			if len(runFile.Mounts) > 0 {
				config.Build.Run[i].Mounts = make([]struct {
					Type   string `json:"type,omitempty" yaml:"type"`
					ID     string `json:"id,omitempty" yaml:"id"`
					Target string `json:"target,omitempty" yaml:"target"`
				}, len(runFile.Mounts))
				for j, mountFile := range runFile.Mounts {
					config.Build.Run[i].Mounts[j].Type = mountFile.Type
					config.Build.Run[i].Mounts[j].ID = mountFile.ID
					config.Build.Run[i].Mounts[j].Target = mountFile.Target
				}
			}
		}
	}

	if cfg.Image != nil {
		config.Image = *cfg.Image
	}
	if cfg.Predict != nil {
		config.Predict = *cfg.Predict
	}
	if cfg.Train != nil {
		config.Train = *cfg.Train
	}
	if cfg.Concurrency != nil {
		config.Concurrency = &Concurrency{}
		if cfg.Concurrency.Max != nil {
			config.Concurrency.Max = *cfg.Concurrency.Max
		}
	}
	config.Environment = cfg.Environment

	// Convert weights
	if len(cfg.Weights) > 0 {
		config.Weights = make([]WeightSource, len(cfg.Weights))
		for i, w := range cfg.Weights {
			config.Weights[i] = WeightSource(w)
		}
	}

	return config, nil
}
