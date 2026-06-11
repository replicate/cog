package main

import (
	"context"

	"github.com/replicate/cog/pkg/cli"
)

// WeightsCmd implements the hidden, experimental "cog weights" command group.
type WeightsCmd struct {
	Import WeightsImportCmd `cmd:"" help:"Build and push weights to a registry."`
	Pull   WeightsPullCmd   `cmd:"" help:"Populate the local weight cache from the registry."`
	Status WeightsStatusCmd `cmd:"" help:"Show the status of configured weights."`
}

// WeightsImportCmd implements "cog weights import".
type WeightsImportCmd struct {
	ConfigFlag `embed:""`

	DryRun  bool     `name:"dry-run" help:"Show what would be imported without making changes."`
	Verbose bool     `name:"verbose" short:"v" help:"Show per-file details."`
	Names   []string `arg:"" optional:"" name:"name" help:"Weight names to import. If omitted, all weights in cog.yaml are imported."`
}

func (cmd *WeightsImportCmd) Run(ctx context.Context) error {
	return cli.RunWeightsImport(ctx, cmd.File, cmd.Names, cmd.DryRun, cmd.Verbose)
}

// WeightsPullCmd implements "cog weights pull".
type WeightsPullCmd struct {
	ConfigFlag `embed:""`

	Verbose bool     `name:"verbose" short:"v" help:"Show per-layer and per-file progress."`
	Names   []string `arg:"" optional:"" name:"name" help:"Weight names to pull. If omitted, all weights in cog.yaml are pulled."`
}

func (cmd *WeightsPullCmd) Run(ctx context.Context) error {
	return cli.RunWeightsPull(ctx, cmd.File, cmd.Names, cmd.Verbose)
}

// WeightsStatusCmd implements "cog weights status".
type WeightsStatusCmd struct {
	ConfigFlag `embed:""`

	JSON    bool `name:"json" help:"Output as JSON."`
	Verbose bool `name:"verbose" short:"v" help:"Show per-layer status."`
}

func (cmd *WeightsStatusCmd) Run(ctx context.Context) error {
	return cli.RunWeightsStatus(ctx, cmd.File, cmd.JSON, cmd.Verbose)
}
