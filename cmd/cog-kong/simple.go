package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/registry"
)

// InitCmd implements the "cog init" command.
type InitCmd struct{}

func (cmd *InitCmd) Run() error { return cli.RunInit() }

// LoginCmd implements the "cog login" command.
type LoginCmd struct {
	TokenStdin bool `name:"token-stdin" help:"Pass login token on stdin instead of opening a browser. You can find your Replicate login token at https://replicate.com/auth/token"`
}

func (cmd *LoginCmd) Run(ctx context.Context, providerReg *provider.Registry) error {
	return cli.RunLogin(ctx, providerReg, cli.LoginOptions{
		TokenStdin: cmd.TokenStdin,
		Host:       global.ReplicateRegistryHost,
	})
}

// DoctorCmd implements the "cog doctor" command.
type DoctorCmd struct {
	ConfigFlag `embed:""`

	Fix bool `name:"fix" help:"Automatically apply fixes."`
}

func (cmd *DoctorCmd) Run(ctx context.Context) error {
	return cli.RunDoctor(ctx, cmd.File, cmd.Fix)
}

// DebugCmd implements the hidden "cog debug" command, which prints a generated
// Dockerfile.
type DebugCmd struct {
	ConfigFlag `embed:""`

	ImageName        string `name:"image-name" help:"The image name to use for the generated Dockerfile."`
	SeparateWeights  bool   `name:"separate-weights" help:"Separate model weights from code in image layers."`
	UseCudaBaseImage string `name:"use-cuda-base-image" default:"auto" enum:"auto,true,false" help:"Use Nvidia CUDA base image."`
	UseCogBaseImage  *bool  `name:"use-cog-base-image" help:"Use pre-built Cog base image for faster cold boots."`

	// Hidden flag for parity with the Cobra debug command. RunDebug ignores it
	// (it is a no-op on both CLIs), but it is accepted for surface parity.
	Timestamp int64 `name:"timestamp" hidden:"" default:"-1" help:"Number of seconds since Epoch to use for the build timestamp."`
}

func (cmd *DebugCmd) Run(ctx context.Context, dockerClient command.Command, regClient registry.Client) error {
	return cli.RunDebug(ctx, dockerClient, regClient, cli.DebugCommandOptions{
		ConfigFilename:   cmd.File,
		ImageName:        cmd.ImageName,
		SeparateWeights:  cmd.SeparateWeights,
		UseCudaBaseImage: cmd.UseCudaBaseImage,
		UseCogBaseImage:  cmd.UseCogBaseImage,
	})
}
