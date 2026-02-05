package model

import (
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Media types for weight artifacts (OCI 1.1 conventions).
const (
	// MediaTypeWeightArtifact is the artifactType for weight manifests.
	MediaTypeWeightArtifact = "application/vnd.cog.weight.v1"
	// MediaTypeWeightConfig is the media type for weight config blobs.
	MediaTypeWeightConfig = "application/vnd.cog.weight.config.v1+json"
	// MediaTypeWeightLayer is the media type for uncompressed weight layers.
	MediaTypeWeightLayer = "application/vnd.cog.weight.layer.v1"
	// MediaTypeWeightLayerGzip is the media type for gzip-compressed weight layers.
	MediaTypeWeightLayerGzip = "application/vnd.cog.weight.layer.v1+gzip"
	// MediaTypeWeightLayerZstd is the media type for zstd-compressed weight layers (future).
	MediaTypeWeightLayerZstd = "application/vnd.cog.weight.layer.v1+zstd"
)

// Annotation keys for weight file layers in OCI manifests.
const (
	AnnotationWeightName             = "vnd.cog.weight.name"
	AnnotationWeightDest             = "vnd.cog.weight.dest"
	AnnotationWeightDigestOriginal   = "vnd.cog.weight.digest.original"
	AnnotationWeightSizeUncompressed = "vnd.cog.weight.size.uncompressed"
)

// WeightSpec declares a weight artifact to be built.
// It implements ArtifactSpec.
type WeightSpec struct {
	name string
	// Source is the local file path to the weight file.
	Source string
	// Target is the container mount path for this weight.
	Target string
}

// NewWeightSpec creates a WeightSpec with the given name, source path, and target mount path.
func NewWeightSpec(name, source, target string) *WeightSpec {
	return &WeightSpec{
		name:   name,
		Source: source,
		Target: target,
	}
}

// Type returns ArtifactTypeWeight.
func (s *WeightSpec) Type() ArtifactType { return ArtifactTypeWeight }

// Name returns the spec's logical name.
func (s *WeightSpec) Name() string { return s.name }

// WeightArtifact is a built weight artifact ready to push as an OCI artifact.
// It implements Artifact.
type WeightArtifact struct {
	name       string
	descriptor v1.Descriptor

	// FilePath is the local file path to the weight data (for pushing layers).
	FilePath string
	// Target is the container mount path for this weight.
	Target string
	// Config is the weight metadata for the config blob.
	Config WeightConfig
}

// NewWeightArtifact creates a WeightArtifact from a build result.
func NewWeightArtifact(name string, desc v1.Descriptor, filePath, target string, cfg WeightConfig) *WeightArtifact {
	return &WeightArtifact{
		name:       name,
		descriptor: desc,
		FilePath:   filePath,
		Target:     target,
		Config:     cfg,
	}
}

// Type returns ArtifactTypeWeight.
func (a *WeightArtifact) Type() ArtifactType { return ArtifactTypeWeight }

// Name returns the artifact's logical name.
func (a *WeightArtifact) Name() string { return a.name }

// Descriptor returns the OCI descriptor for this weight artifact.
func (a *WeightArtifact) Descriptor() v1.Descriptor { return a.descriptor }

// WeightConfig contains metadata about a weight artifact.
// This is serialized as the config blob in the OCI manifest.
// The schema is versioned via SchemaVersion to allow evolution.
type WeightConfig struct {
	SchemaVersion string    `json:"schemaVersion"`
	CogVersion    string    `json:"cogVersion"`
	Name          string    `json:"name"`
	Target        string    `json:"target"`
	Created       time.Time `json:"created"` // RFC 3339 format when serialized to JSON
}
