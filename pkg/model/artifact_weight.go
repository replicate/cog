package model

import (
	"fmt"

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
// The lockfile entry (Entry) is the single source of truth for all
// metadata. The packed layers provide on-disk tar paths for streaming
// during push. The manifest image is cached from the build step so the
// pusher doesn't need to rebuild it.
type WeightArtifact struct {
	descriptor v1.Descriptor
	manifest   v1.Image

	// Entry is the lockfile entry describing this weight's metadata.
	// Must not be mutated after construction.
	Entry WeightLockEntry

	// Layers are the packed tar layers on disk. The pusher reads from
	// these to upload blobs; their metadata (digest, size, mediaType)
	// matches Entry.Layers.
	Layers []PackedLayer
}

// BuildWeightArtifact builds a WeightArtifact from a lockfile entry and
// packed layers. It assembles the manifest, computes the manifest
// descriptor, and backfills entry.Digest — the full ceremony that every
// call site previously did by hand.
func BuildWeightArtifact(entry *WeightLockEntry, layers []PackedLayer) (*WeightArtifact, error) {
	img, err := BuildWeightManifestV1(*entry, layers)
	if err != nil {
		return nil, fmt.Errorf("build weight manifest: %w", err)
	}
	desc, err := descriptorFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("compute manifest descriptor: %w", err)
	}
	entry.Digest = desc.Digest.String()
	return &WeightArtifact{
		descriptor: desc,
		manifest:   img,
		Entry:      *entry,
		Layers:     layers,
	}, nil
}

// NewWeightArtifact creates a WeightArtifact with a pre-built manifest.
// Prefer BuildWeightArtifact for production use; this is for tests that
// need a minimal artifact without a real manifest.
func NewWeightArtifact(entry WeightLockEntry, desc v1.Descriptor, layers []PackedLayer) *WeightArtifact {
	return &WeightArtifact{
		descriptor: desc,
		Entry:      entry,
		Layers:     layers,
	}
}

func (a *WeightArtifact) Type() ArtifactType        { return ArtifactTypeWeight }
func (a *WeightArtifact) Name() string              { return a.Entry.Name }
func (a *WeightArtifact) Descriptor() v1.Descriptor { return a.descriptor }

// Manifest returns the cached OCI manifest image built during
// construction. Returns nil if the artifact was created via
// NewWeightArtifact without a pre-built manifest.
func (a *WeightArtifact) Manifest() v1.Image { return a.manifest }

// TotalSize returns the sum of all layer blob sizes (bytes over the wire).
func (a *WeightArtifact) TotalSize() int64 { return a.Entry.SizeCompressed }
