package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
)

// TrainCmd implements the hidden, deprecated "cog train" command.
type TrainCmd struct {
	RuntimeFlags `embed:""`

	Image        string   `arg:"" optional:"" name:"image" help:"Image to train. If omitted, builds from cog.yaml in the current directory."`
	Input        []string `name:"input" short:"i" help:"Inputs, in the form name=value. If value is prefixed with @, it is read from a file on disk, e.g. -i path=@image.jpg."`
	Output       string   `name:"output" short:"o" default:"weights" help:"Output path."`
	SetupTimeout uint32   `name:"setup-timeout" default:"300" help:"The timeout for a container to setup (in seconds)."`
}

func (cmd *TrainCmd) Run(ctx context.Context, dockerClient command.Command) error {
	return cli.RunTrain(ctx, dockerClient, cli.TrainCommandOptions{
		RuntimeBuildOptions: cmd.Options(),
		Image:               cmd.Image,
		Input:               cmd.Input,
		OutputPath:          cmd.Output,
		SetupTimeout:        cmd.SetupTimeout,
	})
}
