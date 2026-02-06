package kong

// BuildCmd implements `cog build`.
type BuildCmd struct {
	BuildFlags `embed:""`
}

func (c *BuildCmd) Run(g *Globals) error {
	return nil // TODO
}
