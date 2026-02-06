package kong

// RunCmd implements `cog run <command> [arg...]`.
type RunCmd struct {
	Command []string `arg:"" required:"" help:"Command and arguments to run" passthrough:""`

	Publish []string `help:"Publish a container's port to the host" short:"p"`
	Env     []string `help:"Environment variables, in the form name=value" short:"e"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *RunCmd) Run(g *Globals) error {
	return nil // TODO
}
