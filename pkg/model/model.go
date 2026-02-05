package model

import (
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/replicate/cog/pkg/config"
)

// Model represents a Cog model extracted from an image.
type Model struct {
	Image      *ImageArtifact // Underlying OCI image
	Config     *config.Config // Parsed cog.yaml
	Schema     *openapi3.T    // OpenAPI schema
	CogVersion string         // Version of cog used to build

	// ImageFormat describes the OCI structure.
	// Set at build time, determines push strategy.
	// FormatStandalone (default): Traditional single OCI image
	// FormatBundle: OCI Image Index with image + weights artifact
	ImageFormat ModelImageFormat

	// Bundle-specific fields (nil for standalone)
	Index           *Index           // OCI Image Index (populated when inspecting bundle)
	WeightsManifest *WeightsManifest // Weight file metadata (populated for bundles)

	// Artifacts is the collection of all artifacts produced by building this model.
	// Populated by Resolver.Build(). Contains ImageArtifact and WeightArtifact instances.
	Artifacts []Artifact
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

// IsBundle returns true if this model uses the bundle format (OCI Index with weights).
func (m *Model) IsBundle() bool {
	return m.ImageFormat == FormatBundle
}

// GetImageArtifact returns the first ImageArtifact from the artifacts collection,
// or nil if none exists.
func (m *Model) GetImageArtifact() *ImageArtifact {
	for _, a := range m.Artifacts {
		if ia, ok := a.(*ImageArtifact); ok {
			return ia
		}
	}
	return nil
}

// WeightArtifacts returns all WeightArtifact instances from the artifacts collection.
func (m *Model) WeightArtifacts() []*WeightArtifact {
	var weights []*WeightArtifact
	for _, a := range m.Artifacts {
		if wa, ok := a.(*WeightArtifact); ok {
			weights = append(weights, wa)
		}
	}
	return weights
}

// ArtifactsByType returns all artifacts matching the given type.
func (m *Model) ArtifactsByType(t ArtifactType) []Artifact {
	var result []Artifact
	for _, a := range m.Artifacts {
		if a.Type() == t {
			result = append(result, a)
		}
	}
	return result
}
