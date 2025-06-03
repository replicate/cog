package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

var NotFoundError = errors.New("image reference not found")

type RegistryClient struct{}

func NewRegistryClient() Client {
	return &RegistryClient{}
}

func (c *RegistryClient) Inspect(ctx context.Context, imageRef string, platform *Platform) (*ManifestResult, error) {
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return nil, fmt.Errorf("parsing reference: %w", err)
	}

	desc, err := remote.Get(ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		// TODO[md]: map platform to remote.WithPlatform if necessary:
		// remote.WithPlatform(...)
	)
	if err != nil {
		if checkError(err, transport.ManifestUnknownErrorCode, transport.NameUnknownErrorCode) {
			return nil, NotFoundError
		}

		return nil, fmt.Errorf("fetching descriptor: %w", err)
	}

	mediaType := desc.Descriptor.MediaType

	if platform == nil {
		switch mediaType {
		case types.OCIImageIndex, types.DockerManifestList:
			idx, err := desc.ImageIndex()
			if err != nil {
				return nil, fmt.Errorf("loading image index: %w", err)
			}
			indexManifest, err := idx.IndexManifest()
			if err != nil {
				return nil, fmt.Errorf("getting index manifest: %w", err)
			}
			result := &ManifestResult{
				SchemaVersion: indexManifest.SchemaVersion,
				MediaType:     string(mediaType),
			}
			for _, m := range indexManifest.Manifests {
				result.Manifests = append(result.Manifests, PlatformManifest{
					Digest:       m.Digest.String(),
					OS:           m.Platform.OS,
					Architecture: m.Platform.Architecture,
					Variant:      m.Platform.Variant,
				})
			}
			return result, nil

		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			img, err := desc.Image()
			if err != nil {
				return nil, fmt.Errorf("loading image: %w", err)
			}
			manifest, err := img.Manifest()
			if err != nil {
				return nil, fmt.Errorf("getting manifest: %w", err)
			}
			result := &ManifestResult{
				SchemaVersion: manifest.SchemaVersion,
				MediaType:     string(mediaType),
				Config:        manifest.Config.Digest.String(),
			}
			for _, layer := range manifest.Layers {
				result.Layers = append(result.Layers, layer.Digest.String())
			}
			return result, nil
		default:
			return nil, fmt.Errorf("unsupported media type: %s", mediaType)
		}
	}

	// platform is set, we expect a manifest list or error
	if mediaType != types.OCIImageIndex && mediaType != types.DockerManifestList {
		return nil, fmt.Errorf("image is not a manifest list but platform was specified")
	}

	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("loading image index: %w", err)
	}
	indexManifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("getting index manifest: %w", err)
	}

	var matchedDigest string
	for _, m := range indexManifest.Manifests {
		if m.Platform.OS == platform.OS &&
			m.Platform.Architecture == platform.Architecture &&
			m.Platform.Variant == platform.Variant {
			matchedDigest = m.Digest.String()
			break
		}
	}

	if matchedDigest == "" {
		return nil, fmt.Errorf("platform not found in manifest list")
	}

	digestRef, err := name.NewDigest(ref.Context().Name() + "@" + matchedDigest)
	if err != nil {
		return nil, fmt.Errorf("creating digest ref: %w", err)
	}
	manifestDesc, err := remote.Get(digestRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching platform manifest: %w", err)
	}
	img, err := manifestDesc.Image()
	if err != nil {
		return nil, fmt.Errorf("loading platform image: %w", err)
	}
	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("getting manifest: %w", err)
	}
	result := &ManifestResult{
		SchemaVersion: manifest.SchemaVersion,
		MediaType:     string(manifestDesc.Descriptor.MediaType),
		Config:        manifest.Config.Digest.String(),
	}
	for _, layer := range manifest.Layers {
		result.Layers = append(result.Layers, layer.Digest.String())
	}
	return result, nil
}

func (c *RegistryClient) GetImage(ctx context.Context, imageRef string, platform *Platform) (v1.Image, error) {
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return nil, fmt.Errorf("parsing reference: %w", err)
	}

	desc, err := remote.Get(ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching descriptor: %w", err)
	}

	mediaType := desc.Descriptor.MediaType

	// If no platform is specified and it's a single image, return it directly
	if platform == nil {
		switch mediaType {
		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			return desc.Image()
		case types.OCIImageIndex, types.DockerManifestList:
			return nil, fmt.Errorf("platform must be specified for multi-platform image")
		default:
			return nil, fmt.Errorf("unsupported media type: %s", mediaType)
		}
	}

	// For platform-specific requests, we need to handle manifest lists
	if mediaType != types.OCIImageIndex && mediaType != types.DockerManifestList {
		return nil, fmt.Errorf("image is not a manifest list but platform was specified")
	}

	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("loading image index: %w", err)
	}

	indexManifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("getting index manifest: %w", err)
	}

	// Find the matching platform manifest
	var matchedDigest string
	for _, m := range indexManifest.Manifests {
		if m.Platform.OS == platform.OS &&
			m.Platform.Architecture == platform.Architecture &&
			m.Platform.Variant == platform.Variant {
			matchedDigest = m.Digest.String()
			break
		}
	}

	if matchedDigest == "" {
		return nil, fmt.Errorf("platform not found in manifest list")
	}

	// Get the image for the matched digest
	digestRef, err := name.NewDigest(ref.Context().Name() + "@" + matchedDigest)
	if err != nil {
		return nil, fmt.Errorf("creating digest ref: %w", err)
	}

	manifestDesc, err := remote.Get(digestRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching platform manifest: %w", err)
	}

	return manifestDesc.Image()
}

func (c *RegistryClient) Exists(ctx context.Context, imageRef string) (bool, error) {
	if _, err := c.Inspect(ctx, imageRef, nil); err != nil {
		if errors.Is(err, NotFoundError) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func checkError(err error, codes ...transport.ErrorCode) bool {
	if err == nil {
		return false
	}

	var e *transport.Error
	if errors.As(err, &e) {
		for _, diagnosticErr := range e.Errors {
			for _, code := range codes {
				if diagnosticErr.Code == code {
					return true
				}
			}
		}
	}
	return false
}
