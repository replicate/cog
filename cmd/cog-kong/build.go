package main

import (
	"context"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// BuildCmd implements the "cog build" command.
type BuildCmd struct {
	BuildFlags `embed:""`

	Tag string `name:"tag" short:"t" help:"A name for the built image in the form 'repository:tag'."`
}

// Validate is called by Kong after parsing, before Run. It replaces Cobra's PreRunE.
func (cmd *BuildCmd) Validate() error {
	return cmd.ValidateMutualExclusivity()
}

// Run executes the build command.
func (cmd *BuildCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client, src *model.Source) error {
	imageName := src.Config.Image
	if cmd.Tag != "" {
		imageName = cmd.Tag
	}
	if imageName == "" {
		imageName = config.DockerImageName(src.ProjectDir)
	}

	resolver := model.NewResolver(dockerClient, regClient)
	m, err := resolver.Build(ctx, src, cmd.BuildOptions(imageName, nil))
	if err != nil {
		return err
	}

	console.Infof("\nImage built as %s", m.ImageRef())

	return nil
}
