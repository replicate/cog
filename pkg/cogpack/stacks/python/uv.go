package python

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/util"
)

// UvBlock handles uv-based Python dependency management
type UvBlock struct{}

func (b *UvBlock) Name() string { return "uv" }

func (b *UvBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	// always true for now
	return true, nil
}

func (b *UvBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]*plan.Dependency, error) {
	return nil, nil
}

func (b *UvBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	buildStage, err := p.AddStage(plan.PhaseAppDeps, "Init venv", "uv-venv")
	if err != nil {
		return err
	}

	pythonRuntime, ok := p.Dependencies["python"]
	if !ok {
		return fmt.Errorf("python dependency not found")
	}

	buildStage.Operations = []plan.Op{
		plan.Exec{
			Command: fmt.Sprintf("uv venv /venv --python %s", pythonRuntime.ResolvedVersion),
		},
	}

	buildStage.Source = p.GetPhaseResult(plan.PhaseBase)

	util.JSONPrettyPrint(buildStage)

	return nil
}
