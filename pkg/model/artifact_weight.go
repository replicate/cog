package model

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// MediaTypeWeightArtifact is the artifactType on a weight manifest. Layers
// use standard OCI layer media types; this constant lives on the manifest
// itself so clients can distinguish weight manifests from image manifests
// without parsing annotations.
const MediaTypeWeightArtifact = "application/vnd.cog.weight.v1"

// WeightSpec declares a v1 weight artifact to be built from a source
// directory. It implements ArtifactSpec.
type WeightSpec struct {
	name   string
	Source string // source directory, relative to the project dir
	Target string // container mount path
}

// NewWeightSpec creates a WeightSpec with the given name, source directory,
// and target mount path.
func NewWeightSpec(name, source, target string) *WeightSpec {
	return &WeightSpec{name: name, Source: source, Target: target}
}

func (s *WeightSpec) Type() ArtifactType { return ArtifactTypeWeight }
func (s *WeightSpec) Name() string       { return s.name }

// WeightArtifact is a built weight artifact ready to push as an OCI manifest.
// It implements Artifact.
//
// The packer has already written one tar file per layer to disk; Layers
// carries the on-disk paths, digests, sizes, media types, and per-layer
// annotations. The pusher consumes Layers to upload blobs and assemble the
// manifest.
type WeightArtifact struct {
	name       string
	descriptor v1.Descriptor

	Target string
	Layers []LayerResult

	// SetDigest is the content-addressable weight set digest (§2.4).
	SetDigest string
	// ConfigBlob is the serialized config blob JSON (§2.3).
	ConfigBlob []byte
}

// NewWeightArtifact creates a WeightArtifact. desc is the manifest
// descriptor computed by the builder.
func NewWeightArtifact(name string, desc v1.Descriptor, target string, layers []LayerResult, setDigest string, configBlob []byte) *WeightArtifact {
	return &WeightArtifact{
		name:       name,
		descriptor: desc,
		Target:     target,
		Layers:     layers,
		SetDigest:  setDigest,
		ConfigBlob: configBlob,
	}
}

func (a *WeightArtifact) Type() ArtifactType     { return ArtifactTypeWeight }
func (a *WeightArtifact) Name() string           { return a.name }
func (a *WeightArtifact) Descriptor() v1.Descriptor { return a.descriptor }
