package kong

import (
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// BuildCmd implements `cog build`.
type BuildCmd struct {
	BuildFlags `embed:""`
}

func (c *BuildCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	src, err := model.NewSource(c.ConfigFile)
	if err != nil {
		return err
	}

	imageName := src.Config.Image
	if c.Tag != "" {
		imageName = c.Tag
	}
	if imageName == "" {
		imageName = config.DockerImageName(src.ProjectDir)
	}

	if err := config.ValidateModelPythonVersion(src.Config); err != nil {
		return err
	}

	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())
	m, err := resolver.Build(ctx, src, c.BuildFlags.BuildOptions(imageName, nil))
	if err != nil {
		return err
	}

	console.Infof("\nImage built as %s", m.ImageRef())

	return nil
}
