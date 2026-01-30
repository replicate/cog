package model

import (
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/replicate/cog/pkg/config"
)

// Model represents a Cog model extracted from an Image.
type Model struct {
	Image      *Image         // Underlying OCI image
	Config     *config.Config // Parsed cog.yaml
	Schema     *openapi3.T    // OpenAPI schema
	CogVersion string         // Version of cog used to build

	// V2 (OCI Index) fields
	Format          ModelFormat      // OCI structure (image or index)
	Index           *Index           // OCI Image Index (v2 format only, nil for v1)
	WeightsManifest *WeightsManifest // Weight file metadata (v2 format only, nil for v1)
}

// HasGPU returns true if the model requires GPU.
func (m *Model) HasGPU() bool {
	return m.Config != nil && m.Config.Build != nil && m.Config.Build.GPU
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

// IsIndexed returns true if this model uses the OCI Index format (v2).
func (m *Model) IsIndexed() bool {
	return m.Format == ModelFormatIndex
}
