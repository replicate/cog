package python

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
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

func (b *UvBlock) Plan(ctx context.Context, src *project.SourceInfo, composer *plan.Composer) error {
	buildStage, err := composer.AddStage(plan.PhaseAppDeps, "uv-venv", plan.WithName("Init venv"))
	if err != nil {
		return err
	}

	pythonRuntime, ok := composer.GetDependency("python")
	if !ok {
		return fmt.Errorf("python dependency not found")
	}

	buildStage.Operations = []plan.Op{
		plan.Exec{
			Command: fmt.Sprintf("uv venv /venv --python %s", pythonRuntime.ResolvedVersion),
		},
	}

	return nil
}
