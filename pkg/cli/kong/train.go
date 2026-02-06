package kong

// TrainCmd implements `cog train [image]` (hidden).
type TrainCmd struct {
	Image string `arg:"" optional:"" help:"Docker image to train on"`

	Input  []string `help:"Inputs, in the form name=value" short:"i" name:"input"`
	Env    []string `help:"Environment variables, in the form name=value" short:"e"`
	Output string   `help:"Output path" short:"o" default:"weights"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *TrainCmd) Run(g *Globals) error {
	return nil // TODO
}
