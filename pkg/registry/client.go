package registry

import (
	"context"

	v1 "github.com/google/go-containerregistry/pkg/v1"
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

type Client interface {
	// Read methods
	Inspect(ctx context.Context, imageRef string, platform *Platform) (*ManifestResult, error)
	GetImage(ctx context.Context, imageRef string, platform *Platform) (v1.Image, error)
	Exists(ctx context.Context, imageRef string) (bool, error)

	// Write methods for OCI index support
	PushImage(ctx context.Context, ref string, img v1.Image) error
	PushIndex(ctx context.Context, ref string, idx v1.ImageIndex) error

	// WriteLayer pushes a single layer (blob) to a repository.
	// The repo parameter should be a repository reference (e.g., "registry.example.com/user/model").
	WriteLayer(ctx context.Context, repo string, layer v1.Layer) error

	// WriteLayerWithProgress pushes a single layer (blob) to a repository with progress reporting.
	// Progress updates are sent to the provided channel. Use a buffered channel to avoid deadlocks.
	// If progressCh is nil, behaves the same as WriteLayer.
	WriteLayerWithProgress(ctx context.Context, repo string, layer v1.Layer, progressCh chan<- v1.Update) error
}
