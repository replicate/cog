package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weights/store"
)

// Manifest-level annotation keys per spec §2.5 (v1 "run.cog.*" namespace).
const (
	AnnotationV1WeightName      = "run.cog.weight.name"
	AnnotationV1WeightTarget    = "run.cog.weight.target"
	AnnotationV1WeightSetDigest = "run.cog.weight.set-digest"

	// AnnotationV1WeightSizeUncomp carries an uncompressed byte count.
	// It appears in two places:
	//   - On each layer descriptor inside the weight manifest (§2.5):
	//     the uncompressed size of that single layer's contents. Set by
	//     buildWeightManifestV1 from packedLayer.UncompressedSize.
	//   - On the weight descriptor inside the outer OCI index (§2.6):
	//     the sum across all layers — i.e. the total uncompressed size
	//     of the weight. Set by IndexBuilder.AddWeightDescriptor from
	//     the lockfile entry's Size field.
	AnnotationV1WeightSizeUncomp = "run.cog.weight.size.uncompressed"
)

// MediaTypeWeightConfig is the config blob media type per spec §2.1.
const MediaTypeWeightConfig = "application/vnd.cog.weight.config.v1+json"

// buildWeightManifestV1 assembles a v1.Image representing a v1 weight
// manifest from a lockfile entry and the corresponding packed layer
// descriptors. The entry provides all metadata (name, target,
// setDigest, file index for the config blob); the packed layers
// carry their layer plans, which the wrapped fileLayer replays
// against st to produce the on-wire tar bytes during push.
//
// ctx scopes any byte-streaming the manifest's layers do later
// (Compressed/Uncompressed). Pass the push context here so a
// canceled push tears down its layer-streaming goroutines
// promptly. nil is fine for callers that only need manifest digest
// + descriptor without ever reading layer bytes.
//
// st may be nil for callers that only need the manifest digest (no
// blob upload). Push paths must supply a real store.
//
// Layers are canonicalized: the manifest emits them in digest-sorted order,
// regardless of input order. This makes the manifest digest a pure function
// of the layer *set* plus metadata, so cold-pack and warm-cache paths
// (which can produce layers in different orders) produce identical
// manifests. The lockfile is also digest-sorted when serialized, so the
// two canonical forms agree.
//
// The returned image has:
//   - artifactType: application/vnd.cog.weight.v1 (injected via RawManifest override)
//   - config: real config blob (application/vnd.cog.weight.config.v1+json, §2.3)
//   - layers: one descriptor per packedLayer, in digest-sorted order,
//     preserving mediaType, digest, size
//   - annotations: manifest-level weight annotations per spec §2.5
func buildWeightManifestV1(ctx context.Context, entry lockfile.WeightLockEntry, layers []packedLayer, st store.Store) (v1.Image, error) {
	if entry.Name == "" {
		return nil, fmt.Errorf("weight name is required")
	}
	if entry.Target == "" {
		return nil, fmt.Errorf("weight target is required")
	}
	if entry.SetDigest == "" {
		return nil, fmt.Errorf("weight set digest is required")
	}
	if len(entry.Files) == 0 {
		return nil, fmt.Errorf("weight files are required for config blob")
	}
	if len(layers) == 0 {
		return nil, fmt.Errorf("at least one layer is required")
	}

	// Build config blob from the entry's file index. The lockfile is
	// already a superset of the config blob shape (§2.3).
	configBlob, err := buildWeightConfigBlob(entry.Name, entry.Target, entry.SetDigest, entry.Files)
	if err != nil {
		return nil, fmt.Errorf("build config blob: %w", err)
	}

	// Copy and sort by digest so callers' slices aren't reordered as a
	// side effect and the manifest layer order is a pure function of
	// input content.
	sorted := slices.Clone(layers)
	slices.SortFunc(sorted, func(a, b packedLayer) int {
		return strings.Compare(a.Digest.String(), b.Digest.String())
	})

	// Build mutate.Addendum entries. Each addendum wraps our file-backed
	// layer, supplies the media type (used by mutate.Append to build the
	// manifest's layer descriptors), and carries the single layer-level
	// annotation required by spec §2.5.
	//
	// Per spec §2.5 the layer descriptor carries one annotation —
	// run.cog.weight.size.uncompressed — so consumers can make per-layer
	// scheduling/disk decisions without fetching the config blob. All
	// other file-level metadata (paths, per-file sizes, layer mappings)
	// lives in the config blob.
	adds := make([]mutate.Addendum, 0, len(sorted))
	for i, lr := range sorted {
		if lr.Digest.Algorithm == "" || lr.Digest.Hex == "" {
			return nil, fmt.Errorf("layer %d: missing digest", i)
		}
		if lr.Size <= 0 {
			return nil, fmt.Errorf("layer %d (%s): invalid size %d", i, lr.Digest, lr.Size)
		}
		if lr.UncompressedSize <= 0 {
			return nil, fmt.Errorf("layer %d (%s): invalid uncompressed size %d", i, lr.Digest, lr.UncompressedSize)
		}
		if lr.MediaType == "" {
			return nil, fmt.Errorf("layer %d (%s): missing media type", i, lr.Digest)
		}

		adds = append(adds, mutate.Addendum{
			Layer:     newFileLayer(ctx, lr, st),
			MediaType: lr.MediaType,
			Annotations: map[string]string{
				AnnotationV1WeightSizeUncomp: strconv.FormatInt(lr.UncompressedSize, 10),
			},
		})
	}

	// Base on empty.Image, switched to OCI manifest media type.
	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img, err = mutate.Append(img, adds...)
	if err != nil {
		return nil, fmt.Errorf("append weight layers: %w", err)
	}

	// Compute config blob digest + size.
	cfgSum := sha256.Sum256(configBlob)
	cfgDigest := v1.Hash{
		Algorithm: "sha256",
		Hex:       hex.EncodeToString(cfgSum[:]),
	}

	annotations := map[string]string{
		AnnotationV1WeightName:      entry.Name,
		AnnotationV1WeightTarget:    entry.Target,
		AnnotationV1WeightSetDigest: entry.SetDigest,
	}

	// Wrap to inject artifactType, override config to the real config
	// blob descriptor, and attach manifest-level annotations.
	return &weightManifestV1Image{
		Image:       img,
		annotations: annotations,
		configBlob:  configBlob,
		configDesc: v1.Descriptor{
			MediaType: types.MediaType(MediaTypeWeightConfig),
			Size:      int64(len(configBlob)),
			Digest:    cfgDigest,
		},
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
//   - config pointing to the real config blob (§2.3)
//   - manifest-level annotations per spec §2.5
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
	configBlob  []byte
	configDesc  v1.Descriptor

	rawOnce        sync.Once
	rawManifest    []byte
	rawManifestErr error
}

// RawConfigFile returns the weight config blob (§2.3).
func (w *weightManifestV1Image) RawConfigFile() ([]byte, error) {
	return w.configBlob, nil
}

// ArtifactType implements the withArtifactType interface used by partial.Descriptor.
func (w *weightManifestV1Image) ArtifactType() (string, error) {
	return MediaTypeWeightArtifact, nil
}

// Manifest returns the modified manifest with the real config descriptor and
// the v1 weight annotations merged in.
func (w *weightManifestV1Image) Manifest() (*v1.Manifest, error) {
	m, err := w.Image.Manifest()
	if err != nil {
		return nil, err
	}
	mCopy := m.DeepCopy()

	// Override the config descriptor to point to the real config blob (§2.3).
	mCopy.Config = w.configDesc

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

// fileLayer is a v1.Layer that streams its on-wire bytes by re-running
// the packer pipeline against a content-addressed store. There is no
// on-disk tar; the layer reproduces its bytes on demand.
//
// The bytes are deterministic for a given (layerPlan, store) pair, so
// repeated Compressed() calls — once during digest verification, again
// during upload, again during retry — observe identical content.
//
// Unlike tarball.LayerFromFile, fileLayer does not re-compress its
// input: the byte stream IS the on-wire blob. This matches OCI
// "artifact" layers where the blob is whatever the registry stores,
// regardless of the MIME type.
//
// The digest and size are supplied by the packer, not recomputed.
// Recomputing would require streaming the whole tar a third time per
// layer.
type fileLayer struct {
	plan      layerPlan
	digest    v1.Hash
	size      int64
	mediaType types.MediaType
	store     store.Store
	// ctx scopes the streaming goroutine in Compressed(); see
	// newFileLayer for why it lives on the struct.
	ctx context.Context
}

// newFileLayer constructs a fileLayer that streams its bytes from st
// when Compressed/Uncompressed is called.
//
// ctx scopes the streaming goroutine. The v1.Layer interface
// doesn't accept a context (Compressed and Uncompressed are
// zero-arg), so the canonical workaround — also used by
// go-containerregistry's own remote.Layer — is to stash one on the
// struct. When the caller cancels (e.g. user interrupt mid-push),
// streamLayer observes ctx.Err at its next loop boundary and tears
// down the pipe instead of grinding through more bytes for nobody.
func newFileLayer(ctx context.Context, lr packedLayer, st store.Store) *fileLayer {
	return &fileLayer{
		plan:      lr.Plan,
		digest:    lr.Digest,
		size:      lr.Size,
		mediaType: lr.MediaType,
		store:     st,
		ctx:       ctx,
	}
}

// Digest returns the blob digest.
func (l *fileLayer) Digest() (v1.Hash, error) { return l.digest, nil }

// DiffID returns the diff ID for the layer.
//
// For weight artifacts the diff ID is not meaningful (there is no RootFS
// overlay), but partial.Descriptor and mutate.Append both need a non-error
// value. We return the blob digest, matching the pattern used by static.NewLayer.
func (l *fileLayer) DiffID() (v1.Hash, error) { return l.digest, nil }

// Compressed returns the on-wire layer bytes by re-streaming the
// packer pipeline against the store. The store must contain every
// file in l.plan; otherwise the read fails partway through with an
// fs.ErrNotExist-wrapped error.
func (l *fileLayer) Compressed() (io.ReadCloser, error) {
	if l.store == nil {
		return nil, fmt.Errorf("fileLayer: store is nil; cannot stream layer %s", l.digest)
	}
	ctx := l.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	pr, pw := io.Pipe()
	go func() {
		// Errors propagate through pw.CloseWithError so the consumer's
		// next Read returns them. The consumer also controls lifetime
		// from its end: closing the reader makes pw.Write return
		// io.ErrClosedPipe, which streamLayer surfaces as an error.
		_, err := newPacker(nil).streamLayer(ctx, l.store, l.plan, pw)
		_ = pw.CloseWithError(err) //nolint:errcheck // returned err is the only one possible
	}()
	return pr, nil
}

// Uncompressed returns the on-wire layer bytes (same as Compressed for
// weight layers — see fileLayer doc).
func (l *fileLayer) Uncompressed() (io.ReadCloser, error) {
	return l.Compressed()
}

// Size returns the size of the layer blob in bytes.
func (l *fileLayer) Size() (int64, error) { return l.size, nil }

// MediaType returns the layer's OCI media type.
func (l *fileLayer) MediaType() (types.MediaType, error) { return l.mediaType, nil }
