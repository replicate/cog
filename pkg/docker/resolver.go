package docker

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	containerregistryv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/docker/command"
)

type ImageSource string

const (
	ImageSourceLocal  ImageSource = "local"
	ImageSourceRemote ImageSource = "remote"
)

type ResolveMode int

const (
	ResolveModeAuto ResolveMode = iota
	ResolveModePreferLocal
	ResolveModePreferRemote
	ResolveModeLocal
	ResolveModeRemote
)

func ResolveImage(ctx context.Context, ref string, provider command.Command, platform *ocispec.Platform, mode ResolveMode) (containerregistryv1.Image, ImageSource, error) {
	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		return nil, ImageSourceRemote, err
	}

	resolver := &resolver{
		mode:     mode,
		platform: platform,
		provider: provider,
	}

	return resolver.resolve(ctx, parsedRef)
}

type resolver struct {
	provider command.Command
	mode     ResolveMode
	platform *ocispec.Platform
}

func (resolver *resolver) resolve(ctx context.Context, ref name.Reference) (containerregistryv1.Image, ImageSource, error) {
	var preferred, fallback func(ctx context.Context, ref name.Reference) (containerregistryv1.Image, error)
	var preferredSource, fallbackSource ImageSource

	switch resolver.mode {
	case ResolveModePreferLocal, ResolveModeAuto:
		preferred, fallback = resolver.localImage, resolver.remoteImage
		preferredSource, fallbackSource = ImageSourceLocal, ImageSourceRemote
	case ResolveModePreferRemote:
		preferred, fallback = resolver.remoteImage, resolver.localImage
		preferredSource, fallbackSource = ImageSourceRemote, ImageSourceLocal
	case ResolveModeLocal:
		preferred = resolver.localImage
		preferredSource = ImageSourceLocal
	case ResolveModeRemote:
		preferred = resolver.remoteImage
		preferredSource = ImageSourceRemote
	}

	img, err := preferred(ctx, ref)
	if err == nil {
		return img, preferredSource, nil
	}
	if fallback == nil {
		return nil, preferredSource, err
	}

	img, fallbackErr := fallback(ctx, ref)
	if fallbackErr == nil {
		return img, fallbackSource, nil
	}
	return nil, fallbackSource, errors.Join(fallbackErr, err)
}

func (resolver *resolver) localImage(ctx context.Context, ref name.Reference) (containerregistryv1.Image, error) {
	opts := []daemon.Option{
		daemon.WithContext(ctx),
	}

	if clientProvider, ok := resolver.provider.(command.ClientProvider); ok {
		client, err := clientProvider.DockerClient()
		if err != nil {
			return nil, err
		}
		opts = append(opts, daemon.WithClient(client))
	}

	img, err := daemon.Image(ref, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get local image: %w", err)
	}

	return img, nil
}

func (resolver *resolver) remoteImage(ctx context.Context, ref name.Reference) (containerregistryv1.Image, error) {
	opts := []remote.Option{
		remote.WithContext(ctx),
	}
	if resolver.platform != nil {
		opts = append(opts, remote.WithPlatform(containerregistryv1.Platform{
			Architecture: resolver.platform.Architecture,
			OS:           resolver.platform.OS,
			OSVersion:    resolver.platform.OSVersion,
			OSFeatures:   resolver.platform.OSFeatures,
			Variant:      resolver.platform.Variant,
		}))
	}

	img, err := remote.Image(ref, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote image: %w", err)
	}
	return img, nil
}
