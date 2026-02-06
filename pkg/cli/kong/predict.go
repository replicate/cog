package kong

// PredictCmd implements `cog predict [image]`.
type PredictCmd struct {
	Image string `arg:"" optional:"" help:"Docker image to run prediction on"`

	Input        []string `help:"Inputs, in the form name=value" short:"i" name:"input"`
	Output       string   `help:"Output path" short:"o"`
	Env          []string `help:"Environment variables, in the form name=value" short:"e"`
	UseReplToken bool     `help:"Pass REPLICATE_API_TOKEN into the model context" name:"use-replicate-token"`
	JSON         string   `help:"Pass inputs as JSON object, file (@path), or stdin (@-)" name:"json"`
	SetupTimeout uint32   `help:"Container setup timeout in seconds" name:"setup-timeout" default:"300"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *PredictCmd) Run(g *Globals) error {
	return nil // TODO
}
