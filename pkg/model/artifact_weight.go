package model

import (
	"fmt"
	"slices"
	"sort"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model/weightsource"
)

// MediaTypeWeightArtifact is the artifactType on a weight manifest. Layers
// use standard OCI layer media types; this constant lives on the manifest
// itself so clients can distinguish weight manifests from image manifests
// without parsing annotations.
const MediaTypeWeightArtifact = "application/vnd.cog.weight.v1"

// WeightSpec is the normalized, user-declared description of a weight:
// target mount path, source URI, and include/exclude filters. Construct
// via WeightSpecFromConfig or WeightSpecFromLock; compare with Equal.
//
// Include and Exclude are sorted at construction time. They describe a
// set of glob patterns applied by the packer, so order is not part of
// the user's intent — reordering patterns in cog.yaml must not trigger
// a rebuild.
type WeightSpec struct {
	name    string
	Target  string   // container mount path
	URI     string   // normalized source URI (file://./weights, hf://org/repo)
	Include []string // sorted glob patterns
	Exclude []string // sorted glob patterns
}

// WeightSpecFromConfig builds a WeightSpec from a cog.yaml weight entry,
// normalizing the URI and cloning+sorting Include/Exclude. Returns an
// error if the URI is empty or uses an unknown scheme.
func WeightSpecFromConfig(w config.WeightSource) (*WeightSpec, error) {
	uri, err := weightsource.NormalizeURI(w.SourceURI())
	if err != nil {
		return nil, fmt.Errorf("weight %q: %w", w.Name, err)
	}
	var include, exclude []string
	if w.Source != nil {
		include = sortedClone(w.Source.Include)
		exclude = sortedClone(w.Source.Exclude)
	}
	return &WeightSpec{
		name:    w.Name,
		Target:  w.Target,
		URI:     uri,
		Include: include,
		Exclude: exclude,
	}, nil
}

// WeightSpecFromLock extracts the user-intent fields (target, URI,
// include/exclude) from a lockfile entry. Fields are copied as stored:
// no re-normalization. A lockfile whose on-disk form differs from what
// we would write today — whether in URI form, include/exclude order, or
// anything else — must report as drift so the next build rewrites it.
func WeightSpecFromLock(e WeightLockEntry) *WeightSpec {
	return &WeightSpec{
		name:    e.Name,
		Target:  e.Target,
		URI:     e.Source.URI,
		Include: slices.Clone(e.Source.Include),
		Exclude: slices.Clone(e.Source.Exclude),
	}
}

// sortedClone returns a sorted copy of s, or nil if s is nil.
func sortedClone(s []string) []string {
	if s == nil {
		return nil
	}
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}

// Equal reports whether two specs describe the same user intent.
// Name is excluded: callers only compare specs for the same weight name.
func (s *WeightSpec) Equal(other *WeightSpec) bool {
	return s.Target == other.Target &&
		s.URI == other.URI &&
		slices.Equal(s.Include, other.Include) &&
		slices.Equal(s.Exclude, other.Exclude)
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
	Layers []packedLayer
}

// buildWeightArtifact builds a WeightArtifact from a lockfile entry and
// packed layers. It assembles the manifest, computes the manifest
// descriptor, and backfills entry.Digest — the full ceremony that every
// call site previously did by hand.
func buildWeightArtifact(entry *WeightLockEntry, layers []packedLayer) (*WeightArtifact, error) {
	img, err := buildWeightManifestV1(*entry, layers)
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

// newWeightArtifact creates a WeightArtifact with a pre-built manifest.
// Prefer buildWeightArtifact for production use; this is for tests that
// need a minimal artifact without a real manifest.
func newWeightArtifact(entry WeightLockEntry, desc v1.Descriptor, layers []packedLayer) *WeightArtifact {
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
// newWeightArtifact without a pre-built manifest.
func (a *WeightArtifact) Manifest() v1.Image { return a.manifest }

// TotalSize returns the sum of all layer blob sizes (bytes over the wire).
func (a *WeightArtifact) TotalSize() int64 { return a.Entry.SizeCompressed }
