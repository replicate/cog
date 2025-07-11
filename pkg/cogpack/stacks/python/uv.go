package python

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// Stub blocks - these will be implemented in future iterations

// UvBlock handles uv-based Python dependency management
type UvBlock struct{}

func (b *UvBlock) Name() string { return "uv" }
func (b *UvBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return src.FS.GlobExists("pyproject.toml"), nil
}
func (b *UvBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error) {
	return nil, nil
}
func (b *UvBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// TODO: Implement uv dependency management
	return nil
}
