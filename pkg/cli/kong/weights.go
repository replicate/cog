package kong

// WeightsCmd implements `cog weights` (hidden) with subcommands.
type WeightsCmd struct {
	Build WeightsBuildCmd `cmd:"" help:"Generate weights.lock from weight sources in cog.yaml"`
	Push  WeightsPushCmd  `cmd:"" help:"Push weights to a registry"`
}

// WeightsBuildCmd implements `cog weights build`.
type WeightsBuildCmd struct {
	Dest       string `help:"Container path prefix for weights" default:"/cache/"`
	ConfigFile string `help:"Config file path" short:"f" name:"file" default:"cog.yaml"`
}

func (c *WeightsBuildCmd) Run(g *Globals) error {
	return nil // TODO
}

// WeightsPushCmd implements `cog weights push [IMAGE]`.
type WeightsPushCmd struct {
	Image      string `arg:"" optional:"" help:"Image name to determine registry"`
	ConfigFile string `help:"Config file path" short:"f" name:"file" default:"cog.yaml"`
}

func (c *WeightsPushCmd) Run(g *Globals) error {
	return nil // TODO
}
