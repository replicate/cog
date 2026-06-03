package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
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

// Run executes the build command via the shared cli.RunBuild runner.
func (cmd *BuildCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client) error {
	return cli.RunBuild(ctx, dockerClient, regClient, cli.BuildCommandOptions{
		ConfigFilename: cmd.File,
		Tag:            cmd.Tag,
		Flags:          cmd.Options(),
	})
}
