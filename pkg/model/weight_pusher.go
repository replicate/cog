package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/replicate/cog/pkg/registry"
)

// WeightPushProgress reports progress for a weight artifact upload.
type WeightPushProgress struct {
	// Complete is the number of bytes uploaded so far.
	Complete int64
	// Total is the total number of bytes to upload.
	Total int64
}

// WeightPushOptions configures optional behavior for WeightPusher.Push.
type WeightPushOptions struct {
	// ProgressFn is an optional callback for reporting upload progress.
	ProgressFn func(WeightPushProgress)
	// RetryFn is an optional callback for reporting retry attempts.
	// Return false to abort the retry.
	RetryFn func(WeightRetryEvent) bool
}

// WeightRetryEvent reports a retry attempt for a weight file upload.
type WeightRetryEvent struct {
	// Name identifies which file is being retried.
	Name string
	// Attempt is the current retry attempt number (1-indexed).
	Attempt int
	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int
	// Err is the error that caused the retry.
	Err error
	// NextRetryIn is the duration until the next retry attempt.
	NextRetryIn time.Duration
}

// WeightPushResult contains the result of pushing a single weight artifact.
type WeightPushResult struct {
	// Descriptor is the OCI descriptor for the pushed weight manifest.
	Descriptor v1.Descriptor
}

// WeightPusher pushes a WeightArtifact as a proper OCI artifact manifest
// with config blob and tarball layers. The layer blob is pushed via
// registry.WriteLayer (which supports multipart uploads, progress, and retry),
// followed by the manifest via PushImage.
type WeightPusher struct {
	registry registry.Client
}

// NewWeightPusher creates a new WeightPusher.
func NewWeightPusher(reg registry.Client) *WeightPusher {
	return &WeightPusher{registry: reg}
}

// Push pushes a WeightArtifact to the registry as an OCI artifact manifest.
// The layer blob is pushed first via WriteLayer (multipart uploads, progress, retry),
// then the manifest is pushed via PushImage.
// Returns the descriptor of the pushed manifest.
func (p *WeightPusher) Push(ctx context.Context, repo string, artifact *WeightArtifact, opts ...WeightPushOptions) (*WeightPushResult, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	// Merge options (use first if provided)
	var opt WeightPushOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	// Verify the weight file exists
	if _, err := os.Stat(artifact.FilePath); err != nil {
		return nil, fmt.Errorf("weight file %q: %w", artifact.FilePath, err)
	}

	// Build the OCI artifact image (config blob + tarball layer)
	img, err := buildWeightImage(artifact)
	if err != nil {
		return nil, fmt.Errorf("build weight image: %w", err)
	}

	// Extract the layer to push via WriteLayer (gets multipart + progress + retry)
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("get image layers: %w", err)
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("expected 1 layer, got %d", len(layers))
	}
	layer := layers[0]

	// Set up progress channel if callback is provided
	var progressCh chan v1.Update
	var progressDone chan struct{}
	if opt.ProgressFn != nil {
		progressCh = make(chan v1.Update, 100)
		progressDone = make(chan struct{})
		go func() {
			defer close(progressDone)
			for update := range progressCh {
				opt.ProgressFn(WeightPushProgress{
					Complete: update.Complete,
					Total:    update.Total,
				})
			}
		}()
	}

	// Build retry configuration if callback is provided
	var retryConfig *registry.RetryConfig
	if opt.RetryFn != nil {
		retryConfig = &registry.RetryConfig{
			OnRetry: func(event registry.RetryEvent) bool {
				return opt.RetryFn(WeightRetryEvent{
					Name:        artifact.Name(),
					Attempt:     event.Attempt,
					MaxAttempts: event.MaxAttempts,
					Err:         event.Err,
					NextRetryIn: event.NextRetryIn,
				})
			},
		}
	}

	// 1. Push layer blob via WriteLayer (multipart uploads, progress, retry)
	writeErr := p.registry.WriteLayer(ctx, registry.WriteLayerOptions{
		Repo:       repo,
		Layer:      layer,
		ProgressCh: progressCh,
		Retry:      retryConfig,
	})

	// Close the progress channel ourselves — WriteLayer sends to it but does not close it.
	// This unblocks the goroutine's `range progressCh` loop so it can exit cleanly.
	if progressCh != nil {
		close(progressCh)
	}
	if progressDone != nil {
		<-progressDone
	}

	if writeErr != nil {
		return nil, fmt.Errorf("push weight layer: %w", writeErr)
	}

	// 2. Push manifest via PushImage (small payload, no progress needed).
	// The layer blob is already in the registry, so PushImage will skip re-uploading it.
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
