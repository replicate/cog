package model

import v1 "github.com/google/go-containerregistry/pkg/v1"

// ArtifactType identifies the kind of artifact.
type ArtifactType int

const (
	// ArtifactTypeImage is a container image artifact.
	ArtifactTypeImage ArtifactType = iota + 1
	// ArtifactTypeWeight is a model weight artifact.
	ArtifactTypeWeight
)

// String returns the human-readable name of the artifact type.
func (t ArtifactType) String() string {
	switch t {
	case ArtifactTypeImage:
		return "image"
	case ArtifactTypeWeight:
		return "weight"
	default:
		return "unknown"
	}
}

// ArtifactSpec declares what artifact will be produced.
// It contains all inputs needed to build that artifact.
// Specs are derived from analyzing the Source (cog.yaml + project directory).
type ArtifactSpec interface {
	Type() ArtifactType
	Name() string
}

// Artifact is the immutable result of building a spec.
// It contains the OCI descriptor and enough information for a pusher to upload it.
type Artifact interface {
	Type() ArtifactType
	Name() string
	Descriptor() v1.Descriptor
}
