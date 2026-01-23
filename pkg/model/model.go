package model

import (
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/replicate/cog/pkg/config"
)

// Model represents a Cog model extracted from an Image.
type Model struct {
	Image      *Image           // Underlying OCI image
	Config     *config.Config   // Parsed cog.yaml
	Schema     *openapi3.T      // OpenAPI schema
	CogVersion string           // Version of cog used to build
	Weights    *WeightsManifest // Weight file info (optional)
	Runtime    *RuntimeConfig   // Runtime env config
	GitCommit  string           // Git commit (if available)
	GitTag     string           // Git tag (if available)
}

// RuntimeConfig holds runtime environment info.
type RuntimeConfig struct {
	GPU           bool
	CudaVersion   string
	CudnnVersion  string
	PythonVersion string
	TorchVersion  string
	Env           map[string]string
}

// WeightsManifest describes model weights.
type WeightsManifest struct {
	Files []WeightFile
}

// WeightFile represents a single weight file in the manifest.
type WeightFile struct {
	Path   string
	Digest string
	Size   int64
	URL    string // For external weights
}

// HasGPU returns true if the model requires GPU.
func (m *Model) HasGPU() bool {
	return m.Config != nil && m.Config.Build != nil && m.Config.Build.GPU
}

// IsFast returns true if the model uses fast build mode.
func (m *Model) IsFast() bool {
	return m.Config != nil && m.Config.Build != nil && m.Config.Build.Fast
}

// SchemaJSON returns the OpenAPI schema as JSON bytes, or nil if no schema.
func (m *Model) SchemaJSON() ([]byte, error) {
	if m.Schema == nil {
		return nil, nil
	}
	return json.Marshal(m.Schema)
}

// ImageRef returns the image reference string, or empty string if no image.
func (m *Model) ImageRef() string {
	if m.Image == nil {
		return ""
	}
	return m.Image.Reference
}
