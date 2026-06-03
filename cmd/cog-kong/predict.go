package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
)

// predictionFlags groups the flags shared by the predict and run commands.
type predictionFlags struct {
	RuntimeFlags `embed:""`

	Image        string   `arg:"" optional:"" name:"image" help:"Image to run. If omitted, builds from cog.yaml in the current directory."`
	Input        []string `name:"input" short:"i" help:"Inputs, in the form name=value. If value is prefixed with @, it is read from a file on disk, e.g. -i path=@image.jpg."`
	Output       string   `name:"output" short:"o" help:"Output path."`
	JSON         string   `name:"json" help:"Pass inputs as JSON object, read from file (@inputs.json) or via stdin (@-)."`
	UseReplicate bool     `name:"use-replicate-token" help:"Pass REPLICATE_API_TOKEN from local environment into the model context."`
	SetupTimeout uint32   `name:"setup-timeout" default:"300" help:"The timeout for a container to setup (in seconds)."`
}

func (f predictionFlags) options(use string) cli.PredictionCommandOptions {
	return cli.PredictionCommandOptions{
		RuntimeBuildOptions: f.Options(),
		Use:                 use,
		Image:               f.Image,
		Input:               f.Input,
		InputJSON:           f.JSON,
		OutputPath:          f.Output,
		SetupTimeout:        f.SetupTimeout,
		UseReplicateAPI:     f.UseReplicate,
	}
}

// PredictCmd implements the hidden, deprecated "cog predict" command.
type PredictCmd struct {
	predictionFlags `embed:""`
}

func (cmd *PredictCmd) Run(ctx context.Context, dockerClient command.Command) error {
	return cli.RunPrediction(ctx, dockerClient, cmd.options("predict"))
}

// RunCmd implements the "cog run" command.
type RunCmd struct {
	predictionFlags `embed:""`
}

func (cmd *RunCmd) Run(ctx context.Context, dockerClient command.Command) error {
	return cli.RunPrediction(ctx, dockerClient, cmd.options("run"))
}
