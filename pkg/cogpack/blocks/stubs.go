package blocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack"
	"github.com/replicate/cog/pkg/cogpack/core"
)

// Stub blocks - these will be implemented in future iterations

// AptBlock installs system packages
type AptBlock struct{}

func (b *AptBlock) Name() string { return "apt" }
func (b *AptBlock) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return len(src.Config.Build.SystemPackages) > 0, nil
}
func (b *AptBlock) Dependencies(ctx context.Context, src *core.SourceInfo) ([]cogpack.Dependency, error) {
	return nil, nil
}
func (b *AptBlock) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// TODO: Implement apt package installation
	return nil
}

// UvBlock handles uv-based Python dependency management
type UvBlock struct{}

func (b *UvBlock) Name() string { return "uv" }
func (b *UvBlock) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return src.FS.GlobExists("pyproject.toml"), nil
}
func (b *UvBlock) Dependencies(ctx context.Context, src *core.SourceInfo) ([]cogpack.Dependency, error) {
	return nil, nil
}
func (b *UvBlock) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// TODO: Implement uv dependency management
	return nil
}

// PipBlock handles pip-based Python dependency management
type PipBlock struct{}

func (b *PipBlock) Name() string { return "pip" }
func (b *PipBlock) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return src.FS.GlobExists("requirements.txt"), nil
}
func (b *PipBlock) Dependencies(ctx context.Context, src *core.SourceInfo) ([]cogpack.Dependency, error) {
	return nil, nil
}
func (b *PipBlock) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// TODO: Implement pip dependency management
	return nil
}

// TorchBlock handles PyTorch installation
type TorchBlock struct{}

func (b *TorchBlock) Name() string { return "torch" }
func (b *TorchBlock) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	// TODO: Detect if torch is needed by analyzing requirements
	return false, nil
}
func (b *TorchBlock) Dependencies(ctx context.Context, src *core.SourceInfo) ([]cogpack.Dependency, error) {
	return nil, nil
}
func (b *TorchBlock) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// TODO: Implement torch installation
	return nil
}

// CudaBlock handles CUDA dependencies
type CudaBlock struct{}

func (b *CudaBlock) Name() string { return "cuda" }
func (b *CudaBlock) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	// TODO: Detect if CUDA is needed
	return false, nil
}
func (b *CudaBlock) Dependencies(ctx context.Context, src *core.SourceInfo) ([]cogpack.Dependency, error) {
	return nil, nil
}
func (b *CudaBlock) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// TODO: Implement CUDA setup
	return nil
}
