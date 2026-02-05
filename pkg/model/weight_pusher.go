package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/replicate/cog/pkg/registry"
)

// WeightPushResult contains the result of pushing a single weight artifact.
type WeightPushResult struct {
	// Descriptor is the OCI descriptor for the pushed weight manifest.
	Descriptor v1.Descriptor
}

// WeightPusher pushes a WeightArtifact as a proper OCI artifact manifest
// with config blob and tarball layers. This replaces the broken fileBackedLayer
// approach that fails on Docker Hub with 400 Bad Request.
type WeightPusher struct {
	registry registry.Client
}

// NewWeightPusher creates a new WeightPusher.
func NewWeightPusher(reg registry.Client) *WeightPusher {
	return &WeightPusher{registry: reg}
}

// Push pushes a WeightArtifact to the registry as an OCI artifact manifest.
// Returns the descriptor of the pushed manifest.
func (p *WeightPusher) Push(ctx context.Context, repo string, artifact *WeightArtifact) (*WeightPushResult, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	// Verify the weight file exists
	if _, err := os.Stat(artifact.FilePath); err != nil {
		return nil, fmt.Errorf("weight file %q: %w", artifact.FilePath, err)
	}

	// Build the OCI artifact image
	img, err := buildWeightImage(artifact)
	if err != nil {
		return nil, fmt.Errorf("build weight image: %w", err)
	}

	// Push to registry
	if err := p.registry.PushImage(ctx, repo, img); err != nil {
		return nil, fmt.Errorf("push weight manifest: %w", err)
	}

	// Build result descriptor from the pushed image
	desc, err := descriptorFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("compute manifest descriptor: %w", err)
	}

	return &WeightPushResult{Descriptor: desc}, nil
}

// buildWeightImage creates an OCI artifact image with a config blob (WeightConfig JSON)
// and a tarball layer for the weight file.
func buildWeightImage(artifact *WeightArtifact) (v1.Image, error) {
	// 1. Create the base image with OCI manifest media type
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)

	// 2. Create tarball layer from the weight file
	layer, err := tarball.LayerFromFile(artifact.FilePath, tarball.WithMediaType(types.MediaType(MediaTypeWeightLayer)))
	if err != nil {
		return nil, fmt.Errorf("create tarball layer: %w", err)
	}

	// 3. Append the layer
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return nil, fmt.Errorf("append weight layer: %w", err)
	}

	// 4. Serialize the WeightConfig as the config blob
	configJSON, err := json.Marshal(artifact.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal weight config: %w", err)
	}

	// 5. Wrap to set custom config blob, config media type, and artifactType
	return &weightManifestImage{
		Image:      img,
		configBlob: configJSON,
	}, nil
}

// descriptorFromImage computes the v1.Descriptor for a built image manifest.
func descriptorFromImage(img v1.Image) (v1.Descriptor, error) {
	digest, err := img.Digest()
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("get digest: %w", err)
	}

	rawManifest, err := img.RawManifest()
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("get raw manifest: %w", err)
	}

	return v1.Descriptor{
		MediaType: types.OCIManifestSchema1,
		Size:      int64(len(rawManifest)),
		Digest:    digest,
	}, nil
}

// weightOCIManifest extends v1.Manifest with artifactType for OCI 1.1 support.
// go-containerregistry v0.20.5's v1.Manifest struct does not include artifactType,
// so we serialize it ourselves.
type weightOCIManifest struct {
	SchemaVersion int64             `json:"schemaVersion"`
	MediaType     types.MediaType   `json:"mediaType,omitempty"`
	Config        v1.Descriptor     `json:"config"`
	Layers        []v1.Descriptor   `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
}

// weightManifestImage wraps a v1.Image to set a custom config blob with
// the correct media type and artifactType. This produces a proper OCI 1.1
// artifact manifest for weight data.
//
// The raw manifest is cached on first computation to ensure deterministic
// digests across multiple calls (e.g., during remote.Write which calls
// both RawManifest and Digest).
type weightManifestImage struct {
	v1.Image
	configBlob     []byte
	rawManifest    []byte
	rawManifestErr error
	rawOnce        sync.Once
}

// RawConfigFile returns the WeightConfig JSON as the config blob.
func (w *weightManifestImage) RawConfigFile() ([]byte, error) {
	return w.configBlob, nil
}

// Digest computes the digest from the cached raw manifest.
func (w *weightManifestImage) Digest() (v1.Hash, error) {
	raw, err := w.RawManifest()
	if err != nil {
		return v1.Hash{}, err
	}
	h := sha256.Sum256(raw)
	return v1.Hash{
		Algorithm: "sha256",
		Hex:       hex.EncodeToString(h[:]),
	}, nil
}

// ArtifactType implements the withArtifactType interface used by partial.Descriptor.
func (w *weightManifestImage) ArtifactType() (string, error) {
	return MediaTypeWeightArtifact, nil
}

// Manifest returns the modified manifest with custom config descriptor.
func (w *weightManifestImage) Manifest() (*v1.Manifest, error) {
	m, err := w.Image.Manifest()
	if err != nil {
		return nil, err
	}
	// Make a copy to avoid mutating the original
	mCopy := m.DeepCopy()

	// Set config to point to our custom config blob
	configDigest := sha256.Sum256(w.configBlob)
	mCopy.Config = v1.Descriptor{
		MediaType: types.MediaType(MediaTypeWeightConfig),
		Size:      int64(len(w.configBlob)),
		Digest: v1.Hash{
			Algorithm: "sha256",
			Hex:       hex.EncodeToString(configDigest[:]),
		},
	}

	return mCopy, nil
}

// RawManifest serializes our modified manifest with artifactType field.
// The result is cached to ensure deterministic digests across multiple calls.
func (w *weightManifestImage) RawManifest() ([]byte, error) {
	w.rawOnce.Do(func() {
		m, err := w.Manifest()
		if err != nil {
			w.rawManifestErr = err
			return
		}

		// Build the OCI manifest with artifactType (not in v1.Manifest struct)
		ociManifest := weightOCIManifest{
			SchemaVersion: m.SchemaVersion,
			MediaType:     m.MediaType,
			Config:        m.Config,
			Layers:        m.Layers,
			Annotations:   m.Annotations,
			ArtifactType:  MediaTypeWeightArtifact,
		}

		w.rawManifest, w.rawManifestErr = json.Marshal(ociManifest)
	})

	return w.rawManifest, w.rawManifestErr
}
