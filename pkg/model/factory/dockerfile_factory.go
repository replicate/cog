package factory

import (
	"context"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
)

func newDockerfileFactory(provider command.Command) *dockerfileFactory {
	return &dockerfileFactory{
		provider: provider,
	}
}

type dockerfileFactory struct {
	provider command.Command
}

func (f *dockerfileFactory) Build(ctx context.Context, settings BuildSettings) (*model.Model, BuildInfo, error) {
	buildInfo := BuildInfo{
		FactoryBackend: "dockerfile",
	}

	resolveOpts := []model.ResolveOption{
		model.WithProvider(f.provider),
		model.WithResolveMode(docker.ResolveModeLocal),
	}
	registryClient := registry.NewRegistryClient()

	var imageName string
	// if we're building for predict, build a base image instead of a full image
	if settings.PredictBuild {
		buildInfo.BaseImageOnly = true

		// base images don't have a config, so pass it through to the model resolver
		resolveOpts = append(resolveOpts, model.WithConfig(settings.Config))

		baseImageName, err := image.BuildBase(ctx,
			f.provider,
			settings.Config,
			settings.WorkingDir,
			settings.UseCudaBaseImage,
			settings.UseCogBaseImage,
			settings.ProgressOutput,
			registryClient,
		)
		if err != nil {
			return nil, buildInfo, err
		}
		imageName = baseImageName
	} else {
		imageName = settings.Tag
		err := image.Build(ctx,
			settings.Config,
			settings.WorkingDir,
			imageName,
			settings.BuildSecrets,
			settings.NoCache,
			settings.SeparateWeights,
			settings.UseCudaBaseImage,
			settings.ProgressOutput,
			settings.SchemaFile,
			settings.DockerfileFile,
			settings.UseCogBaseImage,
			settings.Strip,
			settings.Precompile,
			settings.Monobase,
			settings.Annotations,
			settings.LocalImage,
			f.provider,
			registryClient,
		)
		if err != nil {
			return nil, buildInfo, err
		}
	}

	model, err := model.Resolve(ctx, imageName, resolveOpts...)
	if err != nil {
		return nil, buildInfo, err
	}

	return model, buildInfo, nil
}
