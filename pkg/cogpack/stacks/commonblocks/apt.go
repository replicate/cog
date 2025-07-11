package commonblocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// AptBlock installs system packages
type AptBlock struct{}

func (b *AptBlock) Name() string { return "apt" }
func (b *AptBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return len(src.Config.Build.SystemPackages) > 0, nil
}
func (b *AptBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error) {
	return nil, nil
}
func (b *AptBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// TODO: Implement apt package installation
	return nil
}
