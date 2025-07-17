//go:build ignore

package model

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	containerregistryv1 "github.com/google/go-containerregistry/pkg/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
)

type ResolveOption func(o *resolver)

func WithPlatform(platform ocispec.Platform) ResolveOption {
	return func(o *resolver) {
		o.platform = &platform
	}
}

func WithResolveMode(mode docker.ResolveMode) ResolveOption {
	return func(o *resolver) {
		o.mode = mode
	}
}

func WithProvider(provider command.Command) ResolveOption {
	return func(o *resolver) {
		o.provider = provider
	}
}

func WithConfig(config *config.Config) ResolveOption {
	return func(o *resolver) {
		o.config = config
	}
}

func WithDefaultRegistry(registry string) ResolveOption {
	return func(o *resolver) {
		o.defaultRegistry = registry
	}
}

func Resolve(ctx context.Context, imageRef string, opts ...ResolveOption) (*Model, error) {
	resolver := &resolver{
		mode: docker.ResolveModeAuto,
	}

	for _, opt := range opts {
		opt(resolver)
	}

	return resolver.resolve(ctx, imageRef)
}

type resolver struct {
	provider command.Command
	platform *ocispec.Platform
	mode     docker.ResolveMode

	// override "docker.io" as the default registry since it implies specific behavior (eg "library" namespace)
	defaultRegistry string

	// overrides that builders can pass in to work around missing metadata in dev-time models
	config *config.Config
}

func (r *resolver) resolve(ctx context.Context, imageRef string) (*Model, error) {
	parseOpts := []name.Option{}
	if r.defaultRegistry != "" {
		parseOpts = append(parseOpts, name.WithDefaultRegistry(r.defaultRegistry))
	}

	ref, err := name.ParseReference(imageRef, parseOpts...)
	if err != nil {
		return nil, err
	}

	img, source, err := docker.ResolveImage(ctx, imageRef, r.provider, r.platform, r.mode)
	if err != nil {
		return nil, err
	}

	model, err := r.modelFromImage(ref, source, img)
	if err != nil {
		return nil, err
	}

	return model, nil
}

func (r *resolver) modelFromImage(ref name.Reference, source docker.ImageSource, img containerregistryv1.Image) (*Model, error) {
	model := &Model{
		Ref:    ref,
		Source: ModelSource(source),
	}

	rawConfig, err := img.RawConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get image config: %w", err)
	}
	if err := json.Unmarshal(rawConfig, &model.Image); err != nil {
		return nil, fmt.Errorf("failed to unmarshal image: %w", err)
	}

	rawManifest, err := img.RawManifest()
	if err != nil {
		return nil, fmt.Errorf("failed to get image manifest: %w", err)
	}
	if err := json.Unmarshal(rawManifest, &model.Manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	// this is a hack to allow base images built with the dockerfile factory to work without the run.cog.config label
	if r.config != nil {
		model.Config = r.config
	} else {
		model.Config, err = cogConfigFromImage(model.Image.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to get cog config from image: %w", err)
		}
	}

	return model, nil
}

func cogConfigFromImage(imageConfig ocispec.ImageConfig) (*config.Config, error) {
	encodedConfig, ok := imageConfig.Labels[LabelCogConfig]
	if !ok {
		return nil, fmt.Errorf("cog config not found in image labels")
	}

	cfg := &config.Config{}
	if err := json.Unmarshal([]byte(encodedConfig), cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cog config: %w", err)
	}
	return cfg, nil
}
