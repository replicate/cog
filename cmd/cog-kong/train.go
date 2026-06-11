package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

// TrainCmd implements the hidden, deprecated "cog train" command.
type TrainCmd struct {
	RuntimeFlags `embed:""`
	EnvFlag      `embed:""`

	Image  string   `arg:"" optional:"" name:"image" help:"Image to train. If omitted, builds from cog.yaml in the current directory."`
	Input  []string `name:"input" short:"i" help:"Inputs, in the form name=value. If value is prefixed with @, it is read from a file on disk, e.g. -i path=@image.jpg."`
	Output string   `name:"output" short:"o" default:"weights" help:"Output path."`
}

func (cmd *TrainCmd) Run(ctx context.Context, dockerClient command.Command) error {
	// Match Cobra's Deprecated notice for the train command.
	console.Warn("Command \"train\" is deprecated, the train command will be removed in a future version of Cog")
	opts := cmd.Options()
	opts.Env = cmd.Env
	return cli.RunTrain(ctx, dockerClient, cli.TrainCommandOptions{
		RuntimeBuildOptions: opts,
		Image:               cmd.Image,
		Input:               cmd.Input,
		OutputPath:          cmd.Output,
	})
}
