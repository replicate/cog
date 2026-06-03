package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/registry"
)

// PushCmd implements the "cog push" command.
type PushCmd struct {
	BuildFlags `embed:""`

	Image string `arg:"" optional:"" help:"Image name to push (e.g. registry.example.com/user/model)."`
}

// Validate is called by Kong after parsing, before Run.
func (cmd *PushCmd) Validate() error {
	return cmd.ValidateMutualExclusivity()
}

// Run executes the push command via the shared cli.RunPush runner.
func (cmd *PushCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client, providerReg *provider.Registry) error {
	return cli.RunPush(ctx, dockerClient, regClient, providerReg, cli.PushCommandOptions{
		ConfigFilename: cmd.File,
		Image:          cmd.Image,
		Flags:          cmd.Options(),
	})
}
