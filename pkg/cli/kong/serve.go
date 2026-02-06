package kong

// ServeCmd implements `cog serve`.
type ServeCmd struct {
	Port int      `help:"Port on which to listen" short:"p" default:"8393"`
	Env  []string `help:"Environment variables, in the form name=value" short:"e"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *ServeCmd) Run(g *Globals) error {
	return nil // TODO
}
