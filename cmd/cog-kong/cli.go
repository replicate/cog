package main

import (
	"os"

	"github.com/alecthomas/kong"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

// Globals holds flags available to every command.
// The AfterApply hook replaces Cobra's PersistentPreRun.
type Globals struct {
	Debug    bool             `name:"debug" short:"d" env:"COG_DEBUG" help:"Show debugging output."`
	NoColor  bool             `name:"no-color" help:"Disable colored output."`
	Help     bool             `name:"help" short:"h" help:"Show context-sensitive help."`
	Registry string           `name:"registry" default:"${registry_default}" env:"COG_REGISTRY_HOST" hidden:"" help:"Registry host."`
	Profile  bool             `name:"profile" hidden:"" help:"Enable profiling."`
	Version  kong.VersionFlag `name:"version" help:"Show version of Cog."`
}

// AfterApply runs after flag parsing, before the command's Run.
// This is the Kong equivalent of Cobra's PersistentPreRun.
func (g *Globals) AfterApply() error {
	if g.Debug {
		global.Debug = true
		console.SetLevel(console.DebugLevel)
	}
	if g.NoColor {
		global.NoColor = true
	}
	if global.NoColor || !console.ShouldUseColor() {
		console.SetColor(false)
	}
	if global.NoColor {
		_ = os.Setenv("NO_COLOR", "1")
	}
	if g.Profile {
		global.ProfilingEnabled = true
	}
	global.ReplicateRegistryHost = g.Registry
	return nil
}
