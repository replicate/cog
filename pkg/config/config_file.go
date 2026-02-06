package config

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v2"
)

// ConfigFile represents the raw cog.yaml as written by users.
// All fields are pointers/omitempty to distinguish "not set" from "set to zero value".
// This struct is only used during parsing - validation produces errors,
// completion produces a Config.
type ConfigFile struct {
	Build       *BuildFile       `json:"build,omitempty" yaml:"build,omitempty"`
	Image       *string          `json:"image,omitempty" yaml:"image,omitempty"`
	Predict     *string          `json:"predict,omitempty" yaml:"predict,omitempty"`
	Train       *string          `json:"train,omitempty" yaml:"train,omitempty"`
	Concurrency *ConcurrencyFile `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Environment []string         `json:"environment,omitempty" yaml:"environment,omitempty"`
	Weights     []WeightFile     `json:"weights,omitempty" yaml:"weights,omitempty"`
}

// BuildFile represents the raw build configuration from cog.yaml.
type BuildFile struct {
	GPU                *bool         `json:"gpu,omitempty" yaml:"gpu,omitempty"`
	PythonVersion      *string       `json:"python_version,omitempty" yaml:"python_version,omitempty"`
	PythonRequirements *string       `json:"python_requirements,omitempty" yaml:"python_requirements,omitempty"`
	Run                []RunItemFile `json:"run,omitempty" yaml:"run,omitempty"`
	SystemPackages     []string      `json:"system_packages,omitempty" yaml:"system_packages,omitempty"`
	CUDA               *string       `json:"cuda,omitempty" yaml:"cuda,omitempty"`
	CuDNN              *string       `json:"cudnn,omitempty" yaml:"cudnn,omitempty"`

	// Deprecated fields - parsed with warnings
	PythonPackages []string `json:"python_packages,omitempty" yaml:"python_packages,omitempty"`
	PreInstall     []string `json:"pre_install,omitempty" yaml:"pre_install,omitempty"`
}

// RunItemFile represents a run command which can be either a string or an object.
type RunItemFile struct {
	Command string      `json:"command,omitempty" yaml:"command,omitempty"`
	Mounts  []MountFile `json:"mounts,omitempty" yaml:"mounts,omitempty"`
}

// MountFile represents a mount configuration in a run command.
type MountFile struct {
	Type   string `json:"type,omitempty" yaml:"type,omitempty"`
	ID     string `json:"id,omitempty" yaml:"id,omitempty"`
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
}

// WeightFile represents a weight source configuration.
type WeightFile struct {
	Source string `json:"source" yaml:"source"`
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
}

// ConcurrencyFile represents concurrency configuration.
type ConcurrencyFile struct {
	Max *int `json:"max,omitempty" yaml:"max,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshaling for RunItemFile
// to support both string and object forms.
func (r *RunItemFile) UnmarshalYAML(unmarshal func(interface{}) error) error {
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

		r.Command = aux.Command
		r.Mounts = make([]MountFile, len(aux.Mounts))
		for i, m := range aux.Mounts {
			r.Mounts[i] = MountFile{
				Type:   m.Type,
				ID:     m.ID,
				Target: m.Target,
			}
		}
	default:
		return fmt.Errorf("unexpected type %T for RunItemFile", v)
	}

	return nil
}

// UnmarshalJSON implements custom JSON unmarshaling for RunItemFile
// to support both string and object forms.
func (r *RunItemFile) UnmarshalJSON(data []byte) error {
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

		r.Command = aux.Command
		r.Mounts = make([]MountFile, len(aux.Mounts))
		for i, m := range aux.Mounts {
			r.Mounts[i] = MountFile{
				Type:   m.Type,
				ID:     m.ID,
				Target: m.Target,
			}
		}
	default:
		return fmt.Errorf("unexpected type %T for RunItemFile", v)
	}

	return nil
}

// Helper functions for working with ConfigFile

// GetGPU returns the GPU setting, defaulting to false if not set.
func (b *BuildFile) GetGPU() bool {
	if b == nil || b.GPU == nil {
		return false
	}
	return *b.GPU
}

// GetPythonVersion returns the Python version, or empty string if not set.
func (b *BuildFile) GetPythonVersion() string {
	if b == nil || b.PythonVersion == nil {
		return ""
	}
	return *b.PythonVersion
}

// GetPythonRequirements returns the Python requirements file path, or empty string if not set.
func (b *BuildFile) GetPythonRequirements() string {
	if b == nil || b.PythonRequirements == nil {
		return ""
	}
	return *b.PythonRequirements
}

// GetCUDA returns the CUDA version, or empty string if not set.
func (b *BuildFile) GetCUDA() string {
	if b == nil || b.CUDA == nil {
		return ""
	}
	return *b.CUDA
}

// GetCuDNN returns the CuDNN version, or empty string if not set.
func (b *BuildFile) GetCuDNN() string {
	if b == nil || b.CuDNN == nil {
		return ""
	}
	return *b.CuDNN
}

// GetMax returns the max concurrency, or 0 if not set.
func (c *ConcurrencyFile) GetMax() int {
	if c == nil || c.Max == nil {
		return 0
	}
	return *c.Max
}
