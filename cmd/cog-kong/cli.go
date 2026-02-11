package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/update"
	"github.com/replicate/cog/pkg/util/console"
)

// Globals holds flags available to every command.
// The AfterApply hook replaces Cobra's PersistentPreRun.
type Globals struct {
	Debug    bool             `name:"debug" short:"d" env:"COG_DEBUG" help:"Show debugging output."`
	Registry string           `name:"registry" default:"${registry_default}" env:"COG_REGISTRY_HOST" hidden:"" help:"Registry host."`
	Profile  bool             `name:"profile" hidden:"" help:"Enable profiling."`
	Version  kong.VersionFlag `name:"version" short:"v" help:"Show version of Cog."`
}

// AfterApply runs after flag parsing, before the command's Run.
// This is the Kong equivalent of Cobra's PersistentPreRun.
func (g *Globals) AfterApply(ctx context.Context) error {
	if g.Debug {
		global.Debug = true
		console.SetLevel(console.DebugLevel)
	}
	if g.Profile {
		global.ProfilingEnabled = true
	}
	global.ReplicateRegistryHost = g.Registry

	if err := update.DisplayAndCheckForRelease(ctx); err != nil {
		console.Debugf("%s", err)
	}
	return nil
}
