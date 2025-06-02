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
}

type Client interface {
	Inspect(ctx context.Context, imageRef string, platform *Platform) (*ManifestResult, error)
	GetImage(ctx context.Context, imageRef string, platform *Platform) (v1.Image, error)
	Exists(ctx context.Context, imageRef string) (bool, error)
}
