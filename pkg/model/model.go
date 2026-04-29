package model

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

// Model represents a Cog model extracted from an image.
type Model struct {
	Image      *ImageArtifact // Underlying OCI image
	Config     *config.Config // Parsed cog.yaml
	Schema     *openapi3.T    // OpenAPI schema
	CogVersion string         // Version of cog used to build

	// Artifacts is the collection of all artifacts produced by building this model.
	// Populated by Resolver.Build(). Contains ImageArtifact instances only.
	Artifacts []Artifact

	// Weights are the model's managed weights, loaded from the lockfile
	// during Build. Each Weight carries all lockfile metadata (name,
	// target, digest, set digest, sizes). The push path uses these to
	// HEAD-check weight manifests in the registry; it never streams
	// layer bytes.
	Weights []Weight
}

// Weight is the model's representation of a managed weight, projected
// from a lockfile entry. Fields mirror lockfile.WeightLockEntry but
// this type belongs to the model domain and carries only what the
// build and push paths need.
type Weight struct {
	Name      string
	Target    string
	Digest    string // OCI manifest digest
	SetDigest string // content-addressable file set identity (spec §2.4)
	Size      int64
	// SizeCompressed is the total compressed (over-the-wire) size.
	SizeCompressed int64
}

// WeightFromLockEntry creates a Weight from a lockfile entry.
func WeightFromLockEntry(e lockfile.WeightLockEntry) Weight {
	return Weight{
		Name:           e.Name,
		Target:         e.Target,
		Digest:         e.Digest,
		SetDigest:      e.SetDigest,
		Size:           e.Size,
		SizeCompressed: e.SizeCompressed,
	}
}

// WeightsFromLockfile loads a lockfile from projectDir and returns
// the corresponding Weight slice. Returns an error if the lockfile
// is missing or corrupt.
func WeightsFromLockfile(projectDir string) ([]Weight, error) {
	lock, err := lockfile.LoadWeightsLock(
		filepath.Join(projectDir, lockfile.WeightsLockFilename),
	)
	if err != nil {
		return nil, fmt.Errorf("load weights.lock: %w", err)
	}
	weights := make([]Weight, len(lock.Weights))
	for i, e := range lock.Weights {
		weights[i] = WeightFromLockEntry(e)
	}
	return weights, nil
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

// IsBundle returns true if this model has managed weights.
func (m *Model) IsBundle() bool {
	return len(m.Weights) > 0
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
