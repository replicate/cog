package providers

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/core"
)

// TorchProvider installs torch+torchaudio+torchvision wheels in a dedicated layer.
type TorchProvider struct {
	Version string // e.g., "2.0.1"
	CUDA    string // e.g., "12.1"; empty means CPU
}

func (p *TorchProvider) Name() string {
	return "torch"
}

func (p *TorchProvider) Configure(ctx context.Context, src *core.SourceInfo) error {
	// if src.Config.Build.CUDA == nil {
	// 	return nil
	// }

	// p.Version = src.Config.Build.Torch.Version
	// p.CUDA = src.Config.Build.Torch.CUDA
	return nil
}

func (p *TorchProvider) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return true, nil
}

func (p *TorchProvider) Plan(ctx context.Context, src *core.SourceInfo, plan *core.Plan) error {
	if p.Version == "" {
		return nil
	}

	// uv respects TORCH_CUDA_ARCH_LIST / UV_AUTO_TORCH_GPU variables.
	envVar := "UV_AUTO_TORCH_GPU=1"
	if p.CUDA == "" {
		envVar = "UV_AUTO_TORCH_GPU=0"
	}

	pkgs := []string{"torch==" + p.Version, "torchaudio", "torchvision"}

	args := []string{"uv", "pip", "install", "--system"}
	args = append(args, pkgs...)

	cmds := []core.Op{
		core.Exec{Shell: true, Args: append([]string{"export", envVar, "&&"}, args...)},
	}

	step := core.Stage{
		Name:     "torch",
		LayerID:  "torch-" + p.Version,
		Commands: cmds,
		Provides: []string{"/usr/local/lib/python"},
	}

	plan.BuildSteps = append(plan.BuildSteps, step)
	return nil
}
