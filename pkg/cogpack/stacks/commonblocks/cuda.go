package commonblocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// CudaBlock handles CUDA dependencies
type CudaBlock struct{}

func (b *CudaBlock) Name() string { return "cuda" }
func (b *CudaBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	// TODO: Detect if CUDA is needed
	return false, nil
}
func (b *CudaBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error) {
	return nil, nil
}
func (b *CudaBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// TODO: Implement CUDA setup
	return nil
}
