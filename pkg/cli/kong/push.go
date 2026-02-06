package kong

// PushCmd implements `cog push [IMAGE]`.
type PushCmd struct {
	Image string `arg:"" optional:"" help:"Image name to push to"`

	BuildFlags `embed:""`
}

func (c *PushCmd) Run(g *Globals) error {
	return nil // TODO
}
