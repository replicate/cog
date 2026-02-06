package kong

// DebugCmd implements `cog debug` (hidden).
type DebugCmd struct {
	SeparateWeights bool   `help:"Separate model weights from code" name:"separate-weights"`
	UseCudaBase     string `help:"Use Nvidia CUDA base image" name:"use-cuda-base-image" default:"auto"`
	Dockerfile      string `help:"Path to a Dockerfile" name:"dockerfile" hidden:""`
	UseCogBase      bool   `help:"Use pre-built Cog base image" name:"use-cog-base-image" default:"true" negatable:""`
	Timestamp       int64  `help:"Epoch seconds for reproducible builds" name:"timestamp" hidden:"" default:"-1"`
	ConfigFile      string `help:"Config file path" short:"f" name:"file" default:"cog.yaml"`
	ImageName       string `help:"Image name for generated Dockerfile" name:"image-name"`
}

func (c *DebugCmd) Run(g *Globals) error {
	return nil // TODO
}
