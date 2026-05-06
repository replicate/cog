package model

import (
	"context"
	"fmt"
	"slices"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
)

// MediaTypeWeightArtifact is the artifactType on a weight manifest. Layers
// use standard OCI layer media types; this constant lives on the manifest
// itself so clients can distinguish weight manifests from image manifests
// without parsing annotations.
const MediaTypeWeightArtifact = "application/vnd.cog.weight.v1"

// SourceSpec describes one source within a weight declaration. Each
// source has its own URI and include/exclude filters.
type SourceSpec struct {
	URI     string   // normalized source URI (file://./weights, hf://org/repo, https://...)
	Include []string // sorted glob patterns
	Exclude []string // sorted glob patterns
}

// WeightSpec is the normalized, user-declared description of a weight:
// target mount path and one or more source specifications. Construct
// via WeightSpecFromConfig or WeightSpecFromLock; compare with Equal.
//
// Include and Exclude within each SourceSpec are sorted at construction
// time. They describe a set of glob patterns applied by the packer, so
// order is not part of the user's intent — reordering patterns in
// cog.yaml must not trigger a rebuild.
type WeightSpec struct {
	name    string
	Target  string       // container mount path
	Sources []SourceSpec // one per source in declaration order
}

// WeightSpecFromConfig builds a WeightSpec from a cog.yaml weight entry,
// normalizing URIs and cloning+sorting Include/Exclude for each source.
// Returns an error if any URI is empty or uses an unknown scheme.
func WeightSpecFromConfig(w config.WeightSource) (*WeightSpec, error) {
	sources := make([]SourceSpec, len(w.Source.Items))
	for i, src := range w.Source.Items {
		uri, err := weightsource.NormalizeURI(src.URI)
		if err != nil {
			return nil, fmt.Errorf("weight %q source[%d]: %w", w.Name, i, err)
		}
		sources[i] = SourceSpec{
			URI:     uri,
			Include: sortedClone(src.Include),
			Exclude: sortedClone(src.Exclude),
		}
	}
	return &WeightSpec{
		name:    w.Name,
		Target:  w.Target,
		Sources: sources,
	}, nil
}

// WeightSpecFromLock extracts the user-intent fields (target, sources)
// from a lockfile entry. Fields are copied as stored: no
// re-normalization. A lockfile whose on-disk form differs from what we
// would write today — whether in URI form, include/exclude order, or
// anything else — must report as drift so the next build rewrites it.
func WeightSpecFromLock(e lockfile.WeightLockEntry) *WeightSpec {
	sources := make([]SourceSpec, len(e.Sources))
	for i, s := range e.Sources {
		sources[i] = SourceSpec{
			URI:     s.URI,
			Include: slices.Clone(s.Include),
			Exclude: slices.Clone(s.Exclude),
		}
	}
	return &WeightSpec{
		name:    e.Name,
		Target:  e.Target,
		Sources: sources,
	}
}

// sortedClone returns a sorted copy of s with whitespace-trimmed elements,
// or nil if s is nil. Trimming normalizes patterns that may have stray
// whitespace from YAML parsing; sorting removes order-dependence so
// reordering patterns in cog.yaml does not trigger a rebuild.
func sortedClone(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = strings.TrimSpace(v)
	}
	slices.Sort(out)
	return out
}

// Equal reports whether two specs describe the same user intent.
// Name is excluded: callers only compare specs for the same weight name.
func (s *WeightSpec) Equal(other *WeightSpec) bool {
	if s.Target != other.Target {
		return false
	}
	if len(s.Sources) != len(other.Sources) {
		return false
	}
	for i := range s.Sources {
		a, b := s.Sources[i], other.Sources[i]
		if a.URI != b.URI {
			return false
		}
		if !slices.Equal(a.Include, b.Include) {
			return false
		}
		if !slices.Equal(a.Exclude, b.Exclude) {
			return false
		}
	}
	return true
}

func (s *WeightSpec) Type() ArtifactType { return ArtifactTypeWeight }
func (s *WeightSpec) Name() string       { return s.name }

// ConfigWeight returns the lockfile-package representation of this spec
// for drift comparison. This is the single mapping point between
// WeightSpec and lockfile.ConfigWeight — adding a user-intent field to
// WeightSpec requires updating this method, and the compiler will
// surface any field mismatches.
func (s *WeightSpec) ConfigWeight() lockfile.ConfigWeight {
	sources := make([]lockfile.ConfigSourceEntry, len(s.Sources))
	for i, src := range s.Sources {
		sources[i] = lockfile.ConfigSourceEntry{
			URI:     src.URI,
			Include: src.Include,
			Exclude: src.Exclude,
		}
	}
	return lockfile.ConfigWeight{
		Name:    s.name,
		Target:  s.Target,
		Sources: sources,
	}
}

// WeightArtifact is a built weight artifact ready to push as an OCI manifest.
// It implements Artifact.
//
// The lockfile entry (Entry) is the single source of truth for all
// metadata. Each layer carries its layerPlan; layer bytes are
// reproduced on demand by streaming from store at push time.
type WeightArtifact struct {
	descriptor v1.Descriptor

	// Entry is the lockfile entry describing this weight's metadata.
	// Must not be mutated after construction.
	Entry lockfile.WeightLockEntry

	// Layers are the packed layer descriptors. The pusher reads bytes
	// for each layer by replaying its layerPlan against store; their
	// metadata (digest, size, mediaType) matches Entry.Layers.
	Layers []packedLayer

	// store is the content-addressed store from which layer bytes are
	// re-streamed during push. Required for any path that reads
	// layer bytes; tests that only inspect Entry/Layers metadata may
	// leave it nil.
	store store.Store
}

// buildWeightArtifact builds a WeightArtifact from a lockfile entry,
// packed layer descriptors, and the store from which the layers can
// be re-streamed during push. It assembles the manifest *for digest
// computation only* (so entry.Digest can be backfilled), then
// discards it: the manifest carries fileLayers wired to a particular
// context, so reusing it across Push calls would defeat
// cancellation. Push rebuilds the manifest with the push context.
func buildWeightArtifact(entry *lockfile.WeightLockEntry, layers []packedLayer, st store.Store) (*WeightArtifact, error) {
	img, err := buildWeightManifestV1(context.Background(), *entry, layers, st)
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
		Entry:      *entry,
		Layers:     layers,
		store:      st,
	}, nil
}

// newWeightArtifact creates a WeightArtifact with a pre-built manifest.
// Prefer buildWeightArtifact for production use; this is for tests that
// need a minimal artifact without a real manifest.
func newWeightArtifact(entry lockfile.WeightLockEntry, desc v1.Descriptor, layers []packedLayer) *WeightArtifact {
	return &WeightArtifact{
		descriptor: desc,
		Entry:      entry,
		Layers:     layers,
	}
}

func (a *WeightArtifact) Type() ArtifactType        { return ArtifactTypeWeight }
func (a *WeightArtifact) Name() string              { return a.Entry.Name }
func (a *WeightArtifact) Descriptor() v1.Descriptor { return a.descriptor }

// TotalSize returns the sum of all layer blob sizes (bytes over the wire).
func (a *WeightArtifact) TotalSize() int64 { return a.Entry.SizeCompressed }
