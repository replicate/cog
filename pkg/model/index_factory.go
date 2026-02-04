// pkg/model/index_factory.go
package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// IndexFactory builds OCI Image Indexes with weights artifacts.
type IndexFactory struct{}

// NewIndexFactory creates a new IndexFactory.
func NewIndexFactory() *IndexFactory {
	return &IndexFactory{}
}

// BuildWeightsArtifact builds an OCI artifact from a weights.lock file.
// The filePaths map provides name→filepath mappings for locating the actual weight files.
// Returns the artifact image and the populated WeightsManifest.
func (f *IndexFactory) BuildWeightsArtifact(ctx context.Context, lockPath string, filePaths map[string]string) (v1.Image, *WeightsManifest, error) {
	lock, err := LoadWeightsLock(lockPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load weights lock: %w", err)
	}

	builder := NewWeightsArtifactBuilder()
	if err := builder.AddLayersFromLock(ctx, lock, filePaths); err != nil {
		return nil, nil, fmt.Errorf("add layers: %w", err)
	}

	artifact, err := builder.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("build artifact: %w", err)
	}

	// Convert lock to manifest (now has computed digests)
	manifest := lock.ToWeightsManifest()

	// Get artifact digest
	digest, err := artifact.Digest()
	if err != nil {
		return nil, nil, fmt.Errorf("get artifact digest: %w", err)
	}
	manifest.Digest = digest.String()

	return artifact, manifest, nil
}

// BuildWeightsArtifactFromManifest builds an OCI artifact from a WeightsManifest.
// This is similar to BuildWeightsArtifact but takes an already-parsed manifest
// instead of a lock file path. Useful for push operations where the manifest
// is already attached to the Model.
// The filePaths map provides name→filepath mappings for locating the actual weight files.
func (f *IndexFactory) BuildWeightsArtifactFromManifest(ctx context.Context, manifest *WeightsManifest, filePaths map[string]string) (v1.Image, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is nil")
	}

	builder := NewWeightsArtifactBuilder()

	// Convert WeightsManifest to WeightsLock format for the builder
	// (both use the same WeightFile structure)
	lock := &WeightsLock{
		Version: "1",
		Created: manifest.Created,
		Files:   make([]WeightFile, len(manifest.Files)),
	}
	copy(lock.Files, manifest.Files)

	if err := builder.AddLayersFromLock(ctx, lock, filePaths); err != nil {
		return nil, fmt.Errorf("add layers: %w", err)
	}

	return builder.Build()
}

// BuildIndex creates an OCI Image Index from a model image and weights artifact.
func (f *IndexFactory) BuildIndex(ctx context.Context, modelImg v1.Image, weightsArtifact v1.Image, platform *Platform) (v1.ImageIndex, error) {
	if modelImg == nil {
		return nil, fmt.Errorf("model image is required")
	}

	imgDigest, err := modelImg.Digest()
	if err != nil {
		return nil, fmt.Errorf("get image digest: %w", err)
	}

	v1Platform := &v1.Platform{
		OS:           platform.OS,
		Architecture: platform.Architecture,
	}
	if platform.Variant != "" {
		v1Platform.Variant = platform.Variant
	}

	builder := NewIndexBuilder()
	builder.SetModelImage(modelImg, v1Platform)
	builder.SetWeightsArtifact(weightsArtifact, imgDigest.String())

	return builder.Build()
}

// WeightsArtifactBuilder builds OCI artifacts containing model weights.
type WeightsArtifactBuilder struct {
	addendums []mutate.Addendum
}

// NewWeightsArtifactBuilder creates a new builder for weights artifacts.
func NewWeightsArtifactBuilder() *WeightsArtifactBuilder {
	return &WeightsArtifactBuilder{}
}

// AddLayerFromFile adds a weight file as a layer to the artifact, streaming from disk.
// The file is read on-demand when the layer is consumed, avoiding loading into memory.
func (b *WeightsArtifactBuilder) AddLayerFromFile(wf WeightFile, filePath string) error {
	annotations := map[string]string{
		AnnotationWeightsName:             wf.Name,
		AnnotationWeightsDest:             wf.Dest,
		AnnotationWeightsDigestOriginal:   wf.DigestOriginal,
		AnnotationWeightsSizeUncompressed: strconv.FormatInt(wf.SizeUncompressed, 10),
	}

	layer := &fileBackedLayer{
		filePath:  filePath,
		digest:    wf.Digest,
		diffID:    wf.DigestOriginal, // For uncompressed, DiffID == original digest
		size:      wf.Size,
		mediaType: types.MediaType(wf.MediaType),
	}

	addendum := mutate.Addendum{
		Layer:       layer,
		Annotations: annotations,
		MediaType:   types.MediaType(wf.MediaType),
	}

	b.addendums = append(b.addendums, addendum)
	return nil
}

// fileBackedLayer implements v1.Layer by streaming from a file on disk.
// This avoids loading the entire layer into memory.
// Layers are stored uncompressed since model weights don't compress well.
type fileBackedLayer struct {
	filePath  string
	digest    string // SHA256 of file content
	diffID    string // For uncompressed layers, same as digest
	size      int64
	mediaType types.MediaType
}

func (l *fileBackedLayer) Digest() (v1.Hash, error) {
	return v1.NewHash(l.digest)
}

func (l *fileBackedLayer) DiffID() (v1.Hash, error) {
	return v1.NewHash(l.diffID)
}

func (l *fileBackedLayer) Compressed() (io.ReadCloser, error) {
	// Layer is uncompressed, so "compressed" just returns the file as-is
	return os.Open(l.filePath)
}

func (l *fileBackedLayer) Uncompressed() (io.ReadCloser, error) {
	return os.Open(l.filePath)
}

func (l *fileBackedLayer) Size() (int64, error) {
	return l.size, nil
}

func (l *fileBackedLayer) MediaType() (types.MediaType, error) {
	return l.mediaType, nil
}

// Build creates the OCI artifact image with all added layers.
func (b *WeightsArtifactBuilder) Build() (v1.Image, error) {
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)

	if len(b.addendums) > 0 {
		var err error
		img, err = mutate.Append(img, b.addendums...)
		if err != nil {
			return nil, fmt.Errorf("appending layers: %w", err)
		}
	}

	return &weightsArtifact{Image: img}, nil
}

// weightsArtifact wraps an image to implement artifact type.
type weightsArtifact struct {
	v1.Image
}

// ArtifactType implements the withArtifactType interface used by partial.ArtifactType.
func (a *weightsArtifact) ArtifactType() (string, error) {
	return MediaTypeWeightsManifest, nil
}

// AddLayersFromLock adds layers for all files in a WeightsLock.
// The filePaths map provides name→filepath mappings for locating the actual weight files.
// Files are stored uncompressed since model weights (safetensors, GGUF, etc.) don't compress well.
func (b *WeightsArtifactBuilder) AddLayersFromLock(ctx context.Context, lock *WeightsLock, filePaths map[string]string) error {
	for i := range lock.Files {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wf := &lock.Files[i]

		filePath, ok := filePaths[wf.Name]
		if !ok {
			return fmt.Errorf("no file path provided for weight %q", wf.Name)
		}

		// Compute digest and size by streaming through the file
		digest, size, err := hashFile(filePath)
		if err != nil {
			return fmt.Errorf("hash weight file %s: %w", wf.Name, err)
		}

		// Update weight file metadata
		wf.Digest = digest
		wf.DigestOriginal = digest // Same for uncompressed
		wf.Size = size
		wf.SizeUncompressed = size
		wf.MediaType = MediaTypeWeightsLayer // Uncompressed

		if err := b.AddLayerFromFile(*wf, filePath); err != nil {
			return fmt.Errorf("add layer %s: %w", wf.Name, err)
		}
	}
	return nil
}

// hashFile computes SHA256 digest and size of a file by streaming.
func hashFile(path string) (digest string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	size, err = io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}

	digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return digest, size, nil
}

// IndexBuilder builds an OCI Image Index containing a model image and optional weights artifact.
type IndexBuilder struct {
	modelImage      v1.Image
	modelPlatform   *v1.Platform
	weightsArtifact v1.Image
	imageDigest     string
}

// NewIndexBuilder creates a new index builder.
func NewIndexBuilder() *IndexBuilder {
	return &IndexBuilder{}
}

// SetModelImage sets the runnable model image.
func (b *IndexBuilder) SetModelImage(img v1.Image, platform *v1.Platform) {
	b.modelImage = img
	b.modelPlatform = platform
}

// SetWeightsArtifact sets the weights artifact.
// imageDigest is the digest of the model image, used in the reference annotation.
func (b *IndexBuilder) SetWeightsArtifact(artifact v1.Image, imageDigest string) {
	b.weightsArtifact = artifact
	b.imageDigest = imageDigest
}

// Build creates the OCI Image Index.
func (b *IndexBuilder) Build() (v1.ImageIndex, error) {
	if b.modelImage == nil {
		return nil, fmt.Errorf("model image not set")
	}

	idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)

	idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
		Add: b.modelImage,
		Descriptor: v1.Descriptor{
			Platform: b.modelPlatform,
		},
	})

	if b.weightsArtifact != nil {
		annotations := map[string]string{
			AnnotationReferenceType: AnnotationValueWeights,
		}
		if b.imageDigest != "" {
			annotations[AnnotationReferenceDigest] = b.imageDigest
		}

		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add: b.weightsArtifact,
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{
					OS:           PlatformUnknown,
					Architecture: PlatformUnknown,
				},
				Annotations: annotations,
			},
		})
	}

	return idx, nil
}
