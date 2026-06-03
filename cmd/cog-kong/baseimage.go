package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
)

// BaseImageCmd implements the experimental "cog base-image" command group.
type BaseImageCmd struct {
	Dockerfile BaseImageDockerfileCmd `cmd:"" help:"Display Cog base image Dockerfile."`
	Build      BaseImageBuildCmd      `cmd:"" help:"Build Cog base image."`
}

// baseImageVersionFlags groups the version-selecting flags shared by the
// base-image subcommands.
type baseImageVersionFlags struct {
	CUDA   string `name:"cuda" help:"CUDA version."`
	Python string `name:"python" help:"Python version."`
	Torch  string `name:"torch" help:"Torch version."`

	// Hidden flags for parity with the Cobra base-image command.
	BreakSystemPackages bool   `name:"break-system-packages" hidden:"" help:"Allow pip to modify uv-managed Python installs."`
	BuildContextDir     string `name:"build-context-dir" hidden:"" help:"Directory for generated Docker build context artifacts."`
	Timestamp           int64  `name:"timestamp" hidden:"" default:"-1" help:"Number of seconds since Epoch to use for the build timestamp."`
}

func (f baseImageVersionFlags) options() cli.BaseImageOptions {
	return cli.BaseImageOptions{
		CUDAVersion:         f.CUDA,
		PythonVersion:       f.Python,
		TorchVersion:        f.Torch,
		BreakSystemPackages: f.BreakSystemPackages,
		BuildContextDir:     f.BuildContextDir,
		Timestamp:           f.Timestamp,
	}
}

// BaseImageDockerfileCmd implements "cog base-image dockerfile".
type BaseImageDockerfileCmd struct {
	baseImageVersionFlags `embed:""`

	NoCache  bool   `name:"no-cache" help:"Do not use cache when building the image."`
	Progress string `name:"progress" default:"${progress_default}" enum:"auto,plain,tty,quiet" help:"Set type of build progress output: ${enum}."`
}

func (cmd *BaseImageDockerfileCmd) Run(ctx context.Context) error {
	opts := cmd.options()
	opts.NoCache = cmd.NoCache
	opts.ProgressOutput = cmd.Progress
	return cli.RunBaseImageDockerfile(ctx, opts)
}

// BaseImageBuildCmd implements "cog base-image build".
type BaseImageBuildCmd struct {
	baseImageVersionFlags `embed:""`
}

func (cmd *BaseImageBuildCmd) Run(ctx context.Context, dockerClient command.Command) error {
	return cli.RunBaseImageBuild(ctx, dockerClient, cmd.options())
}
