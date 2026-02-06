// Package kong implements the Cog CLI using github.com/alecthomas/kong.
//
// This is a parallel implementation alongside the existing Cobra-based CLI in
// pkg/cli/. Both are semantically identical. Once verified, Cobra gets removed
// and this package moves to pkg/cli/.
package kong

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/update"
	"github.com/replicate/cog/pkg/util/console"
)

// CLI is the root command structure for the cog binary.
type CLI struct {
	Globals

	Build   BuildCmd   `cmd:"" help:"Build an image from cog.yaml"`
	Debug   DebugCmd   `cmd:"" help:"Generate a Dockerfile from cog" hidden:""`
	Init    InitCmd    `cmd:"" help:"Configure your project for use with Cog"`
	Login   LoginCmd   `cmd:"" help:"Log in to a container registry"`
	Predict PredictCmd `cmd:"" help:"Run a prediction"`
	Push    PushCmd    `cmd:"" help:"Build and push model in current directory to a Docker registry"`
	Run     RunCmd     `cmd:"" help:"Run a command inside a Docker environment"`
	Serve   ServeCmd   `cmd:"" help:"Run a prediction HTTP server"`
	Train   TrainCmd   `cmd:"" help:"Run a training" hidden:""`
	Weights WeightsCmd `cmd:"" help:"Manage model weights" hidden:""`

	Version kong.VersionFlag `help:"Show version of Cog"`
}

// Globals are flags available to every command.
type Globals struct {
	Debug    bool   `help:"Show debugging output"`
	Profile  bool   `help:"Enable profiling" hidden:""`
	Registry string `help:"Registry host" hidden:"" default:"${registry_default}" env:"COG_REGISTRY_HOST"`
}

// AfterApply is the Kong equivalent of Cobra's PersistentPreRun.
func (g *Globals) AfterApply() error {
	global.Debug = g.Debug
	global.ProfilingEnabled = g.Profile
	if g.Registry != "" {
		global.ReplicateRegistryHost = g.Registry
	}

	if global.Debug {
		console.SetLevel(console.DebugLevel)
	}

	if err := update.DisplayAndCheckForRelease(nil); err != nil {
		console.Debugf("%s", err)
	}

	return nil
}

// Execute parses args and runs the matched command.
func Execute() error {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("cog"),
		kong.Description("Containers for machine learning"),
		kong.UsageOnError(),
		kong.Vars{
			"version":          fmt.Sprintf("%s (built %s)", global.Version, global.BuildTime),
			"registry_default": global.ReplicateRegistryHost,
			"progress_default": progressDefault(),
		},
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
	)
	return ctx.Run(&cli.Globals)
}

// progressDefault computes the default value for --progress flags.
func progressDefault() string {
	if v := os.Getenv("BUILDKIT_PROGRESS"); v != "" {
		return v
	}
	if os.Getenv("TERM") == "dumb" {
		return "plain"
	}
	return "auto"
}
