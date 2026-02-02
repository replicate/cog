// pkg/model/index_factory.go
package model

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// IndexFactory builds OCI Image Indexes with weights artifacts.
type IndexFactory struct{}

// NewIndexFactory creates a new IndexFactory.
func NewIndexFactory() *IndexFactory {
	return &IndexFactory{}
}

// BuildWeightsArtifact builds an OCI artifact from a weights.lock file.
// Returns the artifact image and the populated WeightsManifest.
func (f *IndexFactory) BuildWeightsArtifact(ctx context.Context, lockPath, baseDir string) (v1.Image, *WeightsManifest, error) {
	lock, err := LoadWeightsLock(lockPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load weights lock: %w", err)
	}

	builder := NewWeightsArtifactBuilder()
	if err := builder.AddLayersFromLock(lock, baseDir); err != nil {
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

// BuildIndex creates an OCI Image Index from a model image and weights artifact.
func (f *IndexFactory) BuildIndex(ctx context.Context, modelImg v1.Image, weightsArtifact v1.Image, platform *Platform) (v1.ImageIndex, error) {
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

// AddLayer adds a weight file as a layer to the artifact.
func (b *WeightsArtifactBuilder) AddLayer(wf WeightFile, data []byte) error {
	annotations := map[string]string{
		AnnotationWeightsName:             wf.Name,
		AnnotationWeightsDest:             wf.Dest,
		AnnotationWeightsDigestOriginal:   wf.DigestOriginal,
		AnnotationWeightsSizeUncompressed: strconv.FormatInt(wf.SizeUncompressed, 10),
	}
	if wf.Source != "" {
		annotations[AnnotationWeightsSource] = wf.Source
	}

	layer := static.NewLayer(data, types.MediaType(wf.MediaType))

	addendum := mutate.Addendum{
		Layer:       layer,
		Annotations: annotations,
		MediaType:   types.MediaType(wf.MediaType),
	}

	b.addendums = append(b.addendums, addendum)
	return nil
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
// The baseDir is used to resolve file:// source URLs.
func (b *WeightsArtifactBuilder) AddLayersFromLock(lock *WeightsLock, baseDir string) error {
	for i := range lock.Files {
		wf := &lock.Files[i]

		sourcePath, err := resolveWeightsSource(wf.Source, baseDir)
		if err != nil {
			return fmt.Errorf("resolve source for %s: %w", wf.Name, err)
		}

		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", sourcePath, err)
		}

		// Compute original digest
		originalHash := sha256.Sum256(data)
		wf.DigestOriginal = "sha256:" + hex.EncodeToString(originalHash[:])
		wf.SizeUncompressed = int64(len(data))

		// Compress with gzip
		var compressed bytes.Buffer
		gw := gzip.NewWriter(&compressed)
		if _, err := io.Copy(gw, bytes.NewReader(data)); err != nil {
			return fmt.Errorf("compress %s: %w", wf.Name, err)
		}
		if err := gw.Close(); err != nil {
			return fmt.Errorf("close gzip for %s: %w", wf.Name, err)
		}

		// Compute compressed digest
		compressedHash := sha256.Sum256(compressed.Bytes())
		wf.Digest = "sha256:" + hex.EncodeToString(compressedHash[:])
		wf.Size = int64(compressed.Len())
		wf.MediaType = MediaTypeWeightsLayerGzip

		if err := b.AddLayer(*wf, compressed.Bytes()); err != nil {
			return fmt.Errorf("add layer %s: %w", wf.Name, err)
		}
	}
	return nil
}

// resolveWeightsSource resolves a source URL to an absolute file path.
// Currently only file:// URLs are supported.
func resolveWeightsSource(source, baseDir string) (string, error) {
	if strings.HasPrefix(source, "file://") {
		path := strings.TrimPrefix(source, "file://")
		if !filepath.IsAbs(path) {
			path = strings.TrimPrefix(path, "./")
			path = filepath.Join(baseDir, path)
		}
		return path, nil
	}
	return "", fmt.Errorf("unsupported source scheme: %s (only file:// supported)", source)
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
			AnnotationReferenceType: "weights",
		}
		if b.imageDigest != "" {
			annotations[AnnotationReferenceDigest] = b.imageDigest
		}

		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add: b.weightsArtifact,
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{
					OS:           "unknown",
					Architecture: "unknown",
				},
				Annotations: annotations,
			},
		})
	}

	return idx, nil
}
