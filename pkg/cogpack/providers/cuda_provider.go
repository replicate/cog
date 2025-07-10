package providers

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/core"
)

type CUDAProvider struct{}

func (p *CUDAProvider) Name() string {
	return "cuda"
}

func (p *CUDAProvider) Configure(ctx context.Context, src *core.SourceInfo) error {
	return nil
}

func (p *CUDAProvider) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return true, nil
}

func (p *CUDAProvider) Plan(ctx context.Context, src *core.SourceInfo, plan *core.Plan) error {
	return nil
}
