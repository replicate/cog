package dockerfile

import (
	"context"

	"github.com/replicate/cog/pkg/weightslegacy"
)

type Generator interface {
	GenerateInitialSteps(ctx context.Context) (string, error)
	SetUseCogBaseImage(bool)
	SetUseCogBaseImagePtr(*bool)
	SetBreakSystemPackages(bool)
	GenerateModelBaseWithSeparateWeights(ctx context.Context, imageName string) (string, string, []string, error)
	SetStrip(bool)
	SetPrecompile(bool)
	SetUseCudaBaseImage(string)
	IsUsingCogBaseImage() bool
	BaseImage(ctx context.Context) (string, error)
	GenerateWeightsManifest(ctx context.Context) (*weightslegacy.Manifest, error)
	GenerateDockerfileWithoutSeparateWeights(ctx context.Context) (string, error)
	GenerateModelBase(ctx context.Context) (string, error)
	Name() string
	BuildDir() (string, error)
	BuildContexts() (map[string]string, error)
	BuildCacheDir() string
}
