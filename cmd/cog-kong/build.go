package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// BuildCmd implements the "cog build" command. Timestamp and
// SkipSchemaValidation are build-only hidden flags (push does not expose them),
// matching the Cobra CLI.
type BuildCmd struct {
	BuildFlags `embed:""`

	Tag                  string `name:"tag" short:"t" help:"A name for the built image in the form 'repository:tag'."`
	Timestamp            int64  `name:"timestamp" hidden:"" default:"-1" help:"Number of seconds since Epoch to use for the build timestamp."`
	SkipSchemaValidation bool   `name:"skip-schema-validation" hidden:"" help:"Skip OpenAPI schema generation and validation."`
}

// Validate is called by Kong after parsing, before Run. It replaces Cobra's PreRunE.
func (cmd *BuildCmd) Validate() error {
	return cmd.ValidateMutualExclusivity()
}

// options returns the build flags with the build-only fields applied.
func (cmd *BuildCmd) options() cli.BuildFlagsOptions {
	opts := cmd.Options()
	opts.Timestamp = cmd.Timestamp
	opts.SkipSchemaValidation = cmd.SkipSchemaValidation
	return opts
}

// Run executes the build command via the shared cli.RunBuild runner.
func (cmd *BuildCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client) error {
	return cli.RunBuild(ctx, dockerClient, regClient, cli.BuildCommandOptions{
		ConfigFilename: cmd.File,
		Tag:            cmd.Tag,
		Flags:          cmd.options(),
	})
}
