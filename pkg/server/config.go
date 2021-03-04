package server

import (
	"fmt"

	"gopkg.in/yaml.v2"
)

type Environment struct {
	PythonVersion      string   `yaml:"python_version"`
	PythonRequirements string   `yaml:"python_requirements"`
	PythonPackages     []string `yaml:"python_packages"`
	SystemPackages     []string `yaml:"system_packages"`
	Architectures      []string `yaml:"architectures"`
}

type Config struct {
	Name        string       `yaml:"name"`
	Environment *Environment `yaml:"environment"`
	Model       string       `yaml:"model"`
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
