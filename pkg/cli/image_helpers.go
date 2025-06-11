package cli

import (
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model/factory"
)

func buildSettings(cmd *cobra.Command, cfg *config.Config, isPredict bool, projectDir string) factory.BuildSettings {
	return factory.BuildSettings{
		Tag:        config.DockerImageName(projectDir),
		WorkingDir: projectDir,
		Config:     cfg,
		Platform: ocispec.Platform{
			Architecture: "amd64",
			OS:           "linux",
		},
		Monobase:         buildFast || cfg.Build.Fast,
		NoCache:          buildNoCache,
		BuildSecrets:     buildSecrets,
		SeparateWeights:  buildSeparateWeights,
		UseCudaBaseImage: buildUseCudaBaseImage,
		SchemaFile:       buildSchemaFile,
		DockerfileFile:   buildDockerfileFile,
		Precompile:       buildPrecompile,
		ProgressOutput:   buildProgressOutput,
		Strip:            buildStrip,
		UseCogBaseImage:  DetermineUseCogBaseImage(cmd),
		LocalImage:       buildLocalImage,
		PredictBuild:     isPredict,
		Annotations:      map[string]string{},
	}
}
