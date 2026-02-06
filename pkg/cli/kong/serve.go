package kong

import (
	"strings"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// ServeCmd implements `cog serve`.
type ServeCmd struct {
	Port int      `help:"Port on which to listen" short:"p" default:"8393"`
	Env  []string `help:"Environment variables, in the form name=value" short:"e"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *ServeCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	src, err := model.NewSource(c.ConfigFile)
	if err != nil {
		return err
	}

	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())
	m, err := resolver.BuildBase(ctx, src, c.BuildFlags.BuildBaseOptions())
	if err != nil {
		return err
	}

	gpus := c.GPUFlags.ResolveGPUs(m.HasGPU())

	args := []string{
		"python",
		"--check-hash-based-pycs", "never",
		"-m", "cog.server.http",
		"--await-explicit-shutdown", "true",
	}

	env := propagateRustLog(c.Env)

	runOptions := command.RunOptions{
		Args:    args,
		Env:     env,
		GPUs:    gpus,
		Image:   m.ImageRef(),
		Volumes: []command.Volume{{Source: src.ProjectDir, Destination: "/src"}},
		Workdir: "/src",
		Ports:   []command.Port{{HostPort: c.Port, ContainerPort: 5000}},
	}

	console.Info("")
	console.Infof("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))
	console.Info("")
	console.Infof("Serving at http://127.0.0.1:%d", c.Port)
	console.Info("")

	err = docker.Run(ctx, dockerClient, runOptions)
	if runOptions.GPUs == "all" && c.GPUFlags.IsAutoDetected() && err == docker.ErrMissingDeviceDriver {
		console.Info("Missing device driver, re-trying without GPU")
		runOptions.GPUs = ""
		err = docker.Run(ctx, dockerClient, runOptions)
	}

	return err
}
