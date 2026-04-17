package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"sync"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Manifest-level annotation keys per spec §2.3 (v1 "run.cog.*" namespace).
const (
	AnnotationV1WeightName      = "run.cog.weight.name"
	AnnotationV1WeightTarget    = "run.cog.weight.target"
	AnnotationV1ReferenceType   = "run.cog.reference.type"
	AnnotationV1ReferenceDigest = "run.cog.reference.digest"
	AnnotationOCIImageCreated   = "org.opencontainers.image.created"

	// ReferenceTypeWeights is the value for run.cog.reference.type on weight manifests.
	ReferenceTypeWeights = "weights"
)

// OCI empty descriptor (spec §2.2 config).
//
// The canonical "empty" JSON blob is `{}` (2 bytes). Its sha256 digest is the
// well-known constant below. Per OCI 1.1, weight manifests use the empty
// descriptor as their config rather than a typed blob.
const (
	// MediaTypeOCIEmpty is the media type for the OCI empty descriptor.
	MediaTypeOCIEmpty = "application/vnd.oci.empty.v1+json"

	// emptyBlobSHA256 is the sha256 digest of `{}`.
	emptyBlobSHA256 = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
)

// emptyConfigBlob is the canonical empty JSON blob used as the config for
// weight manifests.
var emptyConfigBlob = []byte(`{}`)

// WeightManifestV1Metadata describes the manifest-level metadata for a v1
// weight manifest.
type WeightManifestV1Metadata struct {
	// Name is the weight name (e.g., "z-image-turbo"). Required.
	Name string
	// Target is the absolute mount path in the container (e.g.,
	// "/src/weights"). Required.
	Target string
	// ReferenceDigest is the digest of the model image this weight belongs
	// to. Optional.
	ReferenceDigest string
	// Created is the time the weight was imported. If zero, time.Now().UTC()
	// is used.
	Created time.Time
}

func (m WeightManifestV1Metadata) validate() error {
	if m.Name == "" {
		return fmt.Errorf("weight name is required")
	}
	if m.Target == "" {
		return fmt.Errorf("weight target is required")
	}
	return nil
}

// annotations returns the manifest-level annotations for this metadata.
func (m WeightManifestV1Metadata) annotations() map[string]string {
	created := m.Created
	if created.IsZero() {
		created = time.Now().UTC()
	}
	anns := map[string]string{
		AnnotationV1WeightName:    m.Name,
		AnnotationV1WeightTarget:  m.Target,
		AnnotationV1ReferenceType: ReferenceTypeWeights,
		AnnotationOCIImageCreated: created.UTC().Format(time.RFC3339),
	}
	if m.ReferenceDigest != "" {
		anns[AnnotationV1ReferenceDigest] = m.ReferenceDigest
	}
	return anns
}

// BuildWeightManifestV1 assembles a v1.Image representing a v1 weight manifest
// from a set of packed tar layers and metadata. The layers are read lazily from
// disk (via the TarPath field on each LayerResult), so very large layers do
// not need to fit in memory.
//
// The returned image has:
//   - artifactType: application/vnd.cog.weight.v1 (injected via RawManifest override)
//   - config: OCI empty descriptor ({} blob, application/vnd.oci.empty.v1+json)
//   - layers: one descriptor per LayerResult, preserving mediaType, digest, size, annotations
//   - annotations: manifest-level weight annotations per spec §2.3
func BuildWeightManifestV1(layers []LayerResult, meta WeightManifestV1Metadata) (v1.Image, error) {
	if err := meta.validate(); err != nil {
		return nil, err
	}
	if len(layers) == 0 {
		return nil, fmt.Errorf("at least one layer is required")
	}

	// Build mutate.Addendum entries. Each addendum wraps our file-backed
	// layer and supplies the per-layer annotations and media type (used by
	// mutate.Append to build the manifest's layer descriptors).
	adds := make([]mutate.Addendum, 0, len(layers))
	for i, lr := range layers {
		if lr.TarPath == "" {
			return nil, fmt.Errorf("layer %d: missing TarPath", i)
		}
		if lr.Digest.Algorithm == "" || lr.Digest.Hex == "" {
			return nil, fmt.Errorf("layer %d (%s): missing digest", i, lr.TarPath)
		}
		if lr.Size <= 0 {
			return nil, fmt.Errorf("layer %d (%s): invalid size %d", i, lr.TarPath, lr.Size)
		}
		if lr.MediaType == "" {
			return nil, fmt.Errorf("layer %d (%s): missing media type", i, lr.TarPath)
		}

		fl := newFileLayer(lr)
		// Clone annotations so downstream mutations on the LayerResult do
		// not bleed into the manifest.
		var anns map[string]string
		if len(lr.Annotations) > 0 {
			anns = maps.Clone(lr.Annotations)
		}
		adds = append(adds, mutate.Addendum{
			Layer:       fl,
			Annotations: anns,
			MediaType:   lr.MediaType,
		})
	}

	// Base on empty.Image, switched to OCI manifest media type.
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img, err := mutate.Append(img, adds...)
	if err != nil {
		return nil, fmt.Errorf("append weight layers: %w", err)
	}

	// Wrap to inject artifactType, override config to the OCI empty
	// descriptor, and attach manifest-level annotations.
	return &weightManifestV1Image{
		Image:       img,
		annotations: meta.annotations(),
	}, nil
}

// weightOCIManifest extends v1.Manifest with artifactType for OCI 1.1 support.
// v1.Manifest in go-containerregistry does not include artifactType at the
// manifest level (only on descriptors), so we serialize it ourselves.
type weightOCIManifest struct {
	SchemaVersion int64             `json:"schemaVersion"`
	MediaType     types.MediaType   `json:"mediaType,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Config        v1.Descriptor     `json:"config"`
	Layers        []v1.Descriptor   `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// weightManifestV1Image wraps a v1.Image to produce a v1 weight manifest with:
//   - artifactType set to application/vnd.cog.weight.v1
//   - config pointing to the OCI empty descriptor
//   - manifest-level annotations per spec §2.3
//
// go-containerregistry's v1.Manifest struct has no ArtifactType field at the
// top level (it lives only on Descriptor). This is a deliberate upstream design
// choice rather than a version lag — upstream main (as of 2026-04) still omits
// it. So we intercept RawManifest() and marshal our own struct that includes
// artifactType. The result is cached via sync.Once so Digest() and
// RawManifest() observe identical bytes, which the registry requires for the
// manifest PUT to succeed.
type weightManifestV1Image struct {
	v1.Image
	annotations map[string]string

	rawOnce        sync.Once
	rawManifest    []byte
	rawManifestErr error
}

// RawConfigFile returns the OCI empty config blob (`{}`).
func (w *weightManifestV1Image) RawConfigFile() ([]byte, error) {
	return emptyConfigBlob, nil
}

// ArtifactType implements the withArtifactType interface used by partial.Descriptor.
func (w *weightManifestV1Image) ArtifactType() (string, error) {
	return MediaTypeWeightArtifact, nil
}

// Manifest returns the modified manifest with the empty config descriptor and
// the v1 weight annotations merged in.
func (w *weightManifestV1Image) Manifest() (*v1.Manifest, error) {
	m, err := w.Image.Manifest()
	if err != nil {
		return nil, err
	}
	mCopy := m.DeepCopy()

	// Override the config descriptor to point to the canonical OCI empty blob.
	mCopy.Config = v1.Descriptor{
		MediaType: types.MediaType(MediaTypeOCIEmpty),
		Size:      int64(len(emptyConfigBlob)),
		Digest: v1.Hash{
			Algorithm: "sha256",
			Hex:       emptyBlobSHA256,
		},
	}

	// Merge in manifest-level annotations. Our annotations win over any upstream.
	if len(w.annotations) > 0 {
		if mCopy.Annotations == nil {
			mCopy.Annotations = make(map[string]string, len(w.annotations))
		}
		maps.Copy(mCopy.Annotations, w.annotations)
	}

	return mCopy, nil
}

// Digest returns the digest of the raw manifest bytes. It must match the
// serialized output of RawManifest so the registry accepts the push.
func (w *weightManifestV1Image) Digest() (v1.Hash, error) {
	raw, err := w.RawManifest()
	if err != nil {
		return v1.Hash{}, err
	}
	sum := sha256.Sum256(raw)
	return v1.Hash{
		Algorithm: "sha256",
		Hex:       hex.EncodeToString(sum[:]),
	}, nil
}

// RawManifest serializes the weight manifest, including the artifactType
// field that v1.Manifest does not carry. The result is cached so Digest()
// and RawManifest() always see identical bytes.
func (w *weightManifestV1Image) RawManifest() ([]byte, error) {
	w.rawOnce.Do(func() {
		m, err := w.Manifest()
		if err != nil {
			w.rawManifestErr = err
			return
		}

		ociManifest := weightOCIManifest{
			SchemaVersion: m.SchemaVersion,
			MediaType:     m.MediaType,
			ArtifactType:  MediaTypeWeightArtifact,
			Config:        m.Config,
			Layers:        m.Layers,
			Annotations:   m.Annotations,
		}
		w.rawManifest, w.rawManifestErr = json.Marshal(ociManifest)
	})
	return w.rawManifest, w.rawManifestErr
}

// fileLayer is a v1.Layer backed by a tar file on disk whose contents are
// already in their final on-wire form (uncompressed tar or gzipped tar).
//
// Unlike tarball.LayerFromFile, fileLayer does not re-compress: it treats the
// file bytes as both the compressed and uncompressed representation for the
// purposes of the OCI layer blob. This is correct for OCI "artifact" layers
// where the blob is whatever the registry stores, regardless of the MIME type.
//
// The digest and size are supplied by the caller (from the packer) rather
// than re-computed, since the file is immutable.
type fileLayer struct {
	path      string
	digest    v1.Hash
	size      int64
	mediaType types.MediaType
}

func newFileLayer(lr LayerResult) *fileLayer {
	return &fileLayer{
		path:      lr.TarPath,
		digest:    lr.Digest,
		size:      lr.Size,
		mediaType: lr.MediaType,
	}
}

// Digest returns the blob digest (sha256 of the file bytes on disk).
func (l *fileLayer) Digest() (v1.Hash, error) { return l.digest, nil }

// DiffID returns the diff ID for the layer.
//
// For weight artifacts the diff ID is not meaningful (there is no RootFS
// overlay), but partial.Descriptor and mutate.Append both need a non-error
// value. We return the blob digest, matching the pattern used by static.NewLayer.
func (l *fileLayer) DiffID() (v1.Hash, error) { return l.digest, nil }

// Compressed returns the file bytes. These are already in their on-wire form,
// so no compression step is applied.
func (l *fileLayer) Compressed() (io.ReadCloser, error) {
	f, err := os.Open(l.path) //nolint:gosec // path is from LayerResult.TarPath, produced by packer
	if err != nil {
		return nil, fmt.Errorf("open layer file %s: %w", l.path, err)
	}
	return f, nil
}

// Uncompressed returns the file bytes (same as Compressed for weight layers).
func (l *fileLayer) Uncompressed() (io.ReadCloser, error) {
	return l.Compressed()
}

// Size returns the size of the layer blob in bytes.
func (l *fileLayer) Size() (int64, error) { return l.size, nil }

// MediaType returns the layer's OCI media type.
func (l *fileLayer) MediaType() (types.MediaType, error) { return l.mediaType, nil }

// init asserts at package load time that the hard-coded empty blob digest
// matches the sha256 of the empty JSON blob. The OCI empty descriptor is a
// well-known constant, but computing it here guards against typos.
func init() {
	sum := sha256.Sum256(emptyConfigBlob)
	got := hex.EncodeToString(sum[:])
	if got != emptyBlobSHA256 {
		panic(fmt.Sprintf("emptyBlobSHA256 mismatch: constant=%s computed=%s", emptyBlobSHA256, got))
	}
}
