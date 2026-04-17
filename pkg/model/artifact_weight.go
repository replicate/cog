package model

import (
	"time"

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

	// Created is stamped into the manifest's org.opencontainers.image.created
	// annotation. Carried on the artifact so the manifest digest recorded
	// by the builder matches the one the pusher sends, given identical
	// layer inputs. Pushers that also set a ReferenceDigest will still
	// produce a different manifest digest — the builder's descriptor is
	// the standalone-push (no-reference) shape.
	Created time.Time
}

// NewWeightArtifact creates a WeightArtifact. desc is the manifest
// descriptor computed by the builder, matching the standalone (no
// ReferenceDigest) manifest shape.
func NewWeightArtifact(name string, desc v1.Descriptor, target string, layers []LayerResult) *WeightArtifact {
	return &WeightArtifact{
		name:       name,
		descriptor: desc,
		Target:     target,
		Layers:     layers,
	}
}

func (a *WeightArtifact) Type() ArtifactType     { return ArtifactTypeWeight }
func (a *WeightArtifact) Name() string           { return a.name }
func (a *WeightArtifact) Descriptor() v1.Descriptor { return a.descriptor }
