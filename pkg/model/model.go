package model

import (
	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/config"
)

// Model holds the resolved model metadata for a Cog model.
type Model struct {
	// Ref is the fully qualified image reference, eg "r8.im/username/modelname"
	Ref name.Reference

	Source ModelSource

	Config *config.Config

	Image    ocispec.Image
	Manifest *ocispec.Manifest
}

// Name returns the name of the model, typically the repository name, eg "username/modelname"
func (m Model) Name() string {
	return m.Ref.Context().RepositoryStr()
}

// ImageRef returns the fully qualified image reference
func (m Model) ImageRef() string {
	return m.Ref.Name()
}

func (m Model) Reference() name.Reference {
	return m.Ref
}

func (m Model) Size() (n int64) {
	for _, layer := range m.Manifest.Layers {
		n += layer.Size
	}
	return
}

type ModelSource string

const (
	ModelSourceLocal  ModelSource = "local"
	ModelSourceRemote ModelSource = "remote"
)
