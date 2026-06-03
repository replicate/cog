package main

import (
	"context"
	"errors"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/registry"
)

// errMissingExecCommand matches Cobra's cobra.MinimumNArgs(1) error message for
// the exec command.
var errMissingExecCommand = errors.New("accepts at least 1 arg(s), received 0")

// RuntimeFlags groups the build/run flags shared by the serve and exec
// commands.
type RuntimeFlags struct {
	ConfigFlag `embed:""`

	Progress string   `name:"progress" default:"${progress_default}" enum:"auto,plain,tty,quiet" help:"Set type of build progress output: ${enum}."`
	CudaBase string   `name:"use-cuda-base-image" default:"auto" enum:"auto,true,false" help:"Use Nvidia CUDA base image."`
	CogBase  *bool    `name:"use-cog-base-image" help:"Use pre-built Cog base image for faster cold boots."`
	GPUs     string   `name:"gpus" help:"GPU devices to add to the container, in the same format as docker run --gpus."`
	Env      []string `name:"env" short:"e" help:"Environment variables, in the form name=value."`
}

// Options converts the Kong runtime flags into cli.RuntimeBuildOptions.
func (f RuntimeFlags) Options() cli.RuntimeBuildOptions {
	return cli.RuntimeBuildOptions{
		ConfigFilename:   f.File,
		ProgressOutput:   f.Progress,
		UseCudaBaseImage: f.CudaBase,
		UseCogBaseImage:  f.CogBase,
		GPUs:             f.GPUs,
		Env:              f.Env,
	}
}

// ServeCmd implements the "cog serve" command.
type ServeCmd struct {
	RuntimeFlags `embed:""`

	Port      int    `name:"port" short:"p" default:"8393" help:"Port on which to listen."`
	UploadURL string `name:"upload-url" help:"Upload URL for file outputs (e.g. https://example.com/upload/)."`
}

func (cmd *ServeCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client) error {
	return cli.RunServe(ctx, dockerClient, regClient, cli.ServeCommandOptions{
		RuntimeBuildOptions: cmd.Options(),
		Port:                cmd.Port,
		UploadURL:           cmd.UploadURL,
	})
}

// ExecCmd implements the "cog exec" command.
type ExecCmd struct {
	RuntimeFlags `embed:""`

	Publish []string `name:"publish" short:"p" help:"Publish a container's port to the host, e.g. -p 8000."`
	Args    []string `arg:"" passthrough:"" name:"command" help:"Command and arguments to execute."`
}

func (cmd *ExecCmd) Validate() error {
	if len(cmd.Args) == 0 {
		return errMissingExecCommand
	}
	return nil
}

func (cmd *ExecCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client) error {
	return cli.RunExec(ctx, dockerClient, regClient, cli.ExecCommandOptions{
		RuntimeBuildOptions: cmd.Options(),
		Args:                cmd.Args,
		Ports:               cmd.Publish,
	})
}
