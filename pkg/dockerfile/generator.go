package dockerfile

import "github.com/replicate/cog/pkg/weights"

type Generator interface {
	generateInitialSteps() (string, error)
	SetUseCogBaseImage(bool)
	SetUseCogBaseImagePtr(*bool)
	GenerateModelBaseWithSeparateWeights(string) (string, string, string, error)
	Cleanup() error
	SetStrip(bool)
	SetPrecompile(bool)
	SetUseCudaBaseImage(string)
	IsUsingCogBaseImage() bool
	BaseImage() (string, error)
	GenerateWeightsManifest() (*weights.Manifest, error)
	GenerateDockerfileWithoutSeparateWeights() (string, error)
	GenerateModelBase() (string, error)
}
