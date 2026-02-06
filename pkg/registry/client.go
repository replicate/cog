package registry

import (
	"context"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Platform struct {
	OS           string
	Architecture string
	Variant      string
}

type PlatformManifest struct {
	Digest       string
	OS           string
	Architecture string
	Variant      string
	Annotations  map[string]string
}

// RetryEvent contains information about a retry attempt.
type RetryEvent struct {
	// Attempt is the current retry attempt number (1-indexed).
	Attempt int
	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int
	// Err is the error that caused the retry.
	Err error
	// NextRetryIn is the duration until the next retry attempt.
	NextRetryIn time.Duration
}

// RetryCallback is called when a retry occurs. Return false to abort retrying.
type RetryCallback func(event RetryEvent) bool

// RetryConfig configures retry behavior for registry operations.
type RetryConfig struct {
	// Backoff configures the exponential backoff for retries.
	// If nil, the default backoff from go-containerregistry is used (3 attempts, 1s initial, 3x factor).
	Backoff *remote.Backoff
	// OnRetry is called when a retry occurs. If nil, no callback is invoked.
	// The callback receives information about the retry attempt.
	OnRetry RetryCallback
}

// WriteLayerOptions configures the WriteLayer operation.
type WriteLayerOptions struct {
	// Repo is the repository to push to.
	Repo string
	// Layer is the layer to push.
	Layer v1.Layer
	// ProgressCh receives progress updates. Use a buffered channel to avoid deadlocks.
	// If nil, no progress updates are sent.
	ProgressCh chan<- v1.Update
	// Retry configures retry behavior. If nil, default retry behavior is used
	// (5 attempts with exponential backoff starting at 2 seconds).
	Retry *RetryConfig
}

type Client interface {
	// Read methods
	Inspect(ctx context.Context, imageRef string, platform *Platform) (*ManifestResult, error)
	GetImage(ctx context.Context, imageRef string, platform *Platform) (v1.Image, error)
	Exists(ctx context.Context, imageRef string) (bool, error)

	// GetDescriptor returns the OCI descriptor for an image reference without downloading
	// the full image. This is a lightweight HEAD request useful for building OCI indexes
	// from already-pushed manifests.
	GetDescriptor(ctx context.Context, imageRef string) (v1.Descriptor, error)

	// Write methods for OCI index support
	PushImage(ctx context.Context, ref string, img v1.Image) error
	PushIndex(ctx context.Context, ref string, idx v1.ImageIndex) error

	// WriteLayer pushes a single layer (blob) to a repository with retry and optional progress reporting.
	// This method handles transient failures automatically with exponential backoff.
	// Use WriteLayerOptions to configure progress reporting and retry callbacks.
	WriteLayer(ctx context.Context, opts WriteLayerOptions) error
}
