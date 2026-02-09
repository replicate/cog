package config

import (
	"encoding/json"
	"fmt"

	"go.yaml.in/yaml/v4"
)

// configFile represents the raw cog.yaml as written by users.
// All fields are pointers/omitempty to distinguish "not set" from "set to zero value".
// This struct is only used during parsing - validation produces errors,
// completion produces a Config.
type configFile struct {
	Build       *buildFile       `json:"build,omitempty" yaml:"build,omitempty"`
	Image       *string          `json:"image,omitempty" yaml:"image,omitempty"`
	Predict     *string          `json:"predict,omitempty" yaml:"predict,omitempty"`
	Train       *string          `json:"train,omitempty" yaml:"train,omitempty"`
	Concurrency *concurrencyFile `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Environment []string         `json:"environment,omitempty" yaml:"environment,omitempty"`
	Weights     []weightFile     `json:"weights,omitempty" yaml:"weights,omitempty"`
}

// buildFile represents the raw build configuration from cog.yaml.
type buildFile struct {
	GPU                *bool         `json:"gpu,omitempty" yaml:"gpu,omitempty"`
	PythonVersion      *string       `json:"python_version,omitempty" yaml:"python_version,omitempty"`
	PythonRequirements *string       `json:"python_requirements,omitempty" yaml:"python_requirements,omitempty"`
	Run                []runItemFile `json:"run,omitempty" yaml:"run,omitempty"`
	SystemPackages     []string      `json:"system_packages,omitempty" yaml:"system_packages,omitempty"`
	CUDA               *string       `json:"cuda,omitempty" yaml:"cuda,omitempty"`
	CuDNN              *string       `json:"cudnn,omitempty" yaml:"cudnn,omitempty"`

	// Deprecated fields - parsed with warnings
	PythonPackages []string `json:"python_packages,omitempty" yaml:"python_packages,omitempty"`
	PreInstall     []string `json:"pre_install,omitempty" yaml:"pre_install,omitempty"`
}

// runItemFile represents a run command which can be either a string or an object.
type runItemFile struct {
	Command string      `json:"command,omitempty" yaml:"command,omitempty"`
	Mounts  []mountFile `json:"mounts,omitempty" yaml:"mounts,omitempty"`
}

// mountFile represents a mount configuration in a run command.
type mountFile struct {
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`
	ID     string `json:"id,omitempty" yaml:"id,omitempty"`
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
}

// weightFile represents a weight source configuration.
type weightFile struct {
	Source string `json:"source" yaml:"source"`
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
}

// concurrencyFile represents concurrency configuration.
type concurrencyFile struct {
	Max *int `json:"max,omitempty" yaml:"max,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshaling for runItemFile
// to support both string and object forms.
func (r *runItemFile) UnmarshalYAML(unmarshal func(any) error) error {
	var commandOrMap any
	if err := unmarshal(&commandOrMap); err != nil {
		return err
	}

	switch v := commandOrMap.(type) {
	case string:
		r.Command = v
	case map[string]any:
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

		r.Command = aux.Command
		r.Mounts = make([]mountFile, len(aux.Mounts))
		for i, m := range aux.Mounts {
			r.Mounts[i] = mountFile{
				Type:   m.Type,
				ID:     m.ID,
				Target: m.Target,
			}
		}
	default:
		return fmt.Errorf("unexpected type %T for runItemFile", v)
	}

	return nil
}

// UnmarshalJSON implements custom JSON unmarshaling for runItemFile
// to support both string and object forms.
func (r *runItemFile) UnmarshalJSON(data []byte) error {
	var commandOrMap any
	if err := json.Unmarshal(data, &commandOrMap); err != nil {
		return err
	}

	switch v := commandOrMap.(type) {
	case string:
		r.Command = v
	case map[string]any:
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

		r.Command = aux.Command
		r.Mounts = make([]mountFile, len(aux.Mounts))
		for i, m := range aux.Mounts {
			r.Mounts[i] = mountFile{
				Type:   m.Type,
				ID:     m.ID,
				Target: m.Target,
			}
		}
	default:
		return fmt.Errorf("unexpected type %T for runItemFile", v)
	}

	return nil
}

// Helper functions for working with configFile

// GetGPU returns the GPU setting, defaulting to false if not set.
func (b *buildFile) GetGPU() bool {
	if b == nil || b.GPU == nil {
		return false
	}
	return *b.GPU
}
