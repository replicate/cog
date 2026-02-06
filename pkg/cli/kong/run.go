package kong

import (
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// RunCmd implements `cog run <command> [arg...]`.
type RunCmd struct {
	Command []string `arg:"" required:"" help:"Command and arguments to run" passthrough:""`

	Publish []string `help:"Publish a container's port to the host" short:"p"`
	Env     []string `help:"Environment variables, in the form name=value" short:"e"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *RunCmd) Run(g *Globals) error {
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
	env := propagateRustLog(c.Env)

	runOptions := command.RunOptions{
		Args:    c.Command,
		Env:     env,
		GPUs:    gpus,
		Image:   m.ImageRef(),
		Volumes: []command.Volume{{Source: src.ProjectDir, Destination: "/src"}},
		Workdir: "/src",
	}

	for _, portString := range c.Publish {
		port, err := strconv.Atoi(portString)
		if err != nil {
			return err
		}
		runOptions.Ports = append(runOptions.Ports, command.Port{HostPort: port, ContainerPort: port})
	}

	console.Info("")
	console.Infof("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(c.Command, " "))

	err = docker.Run(ctx, dockerClient, runOptions)
	if runOptions.GPUs == "all" && c.GPUFlags.IsAutoDetected() && err == docker.ErrMissingDeviceDriver {
		console.Info("Missing device driver, re-trying without GPU")
		runOptions.GPUs = ""
		err = docker.Run(ctx, dockerClient, runOptions)
	}

	return err
}
