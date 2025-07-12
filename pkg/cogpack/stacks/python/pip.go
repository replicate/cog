package python

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// PipBlock handles pip-based Python dependency management
type PipBlock struct{}

func (b *PipBlock) Name() string { return "pip" }
func (b *PipBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return src.FS.GlobExists("requirements.txt"), nil
}
func (b *PipBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error) {
	return nil, nil
}
func (b *PipBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// Build stage - install dependencies in build image
	buildStage, err := p.AddStage(plan.PhaseAppDeps, "Install Python Dependencies", "pip-install")
	if err != nil {
		return err
	}

	// Build from the build base
	buildStage.Source = p.GetPhaseResult(plan.PhaseBase)
	buildStage.Operations = []plan.Op{
		// Copy requirements.txt first
		plan.Copy{
			From: plan.Input{Local: "context"},
			Src:  []string{"requirements.txt"},
			Dest: "/tmp/requirements.txt",
		},
		// Install dependencies
		plan.Exec{
			Command: "pip install --no-cache-dir -r /tmp/requirements.txt",
		},
	}
	buildStage.Provides = []string{"python-deps"}

	// Export stage - copy installed packages to runtime image
	exportStage, err := p.AddStage(plan.ExportPhaseRuntime, "Copy Python Dependencies", "pip-export")
	if err != nil {
		return err
	}

	// Build from the runtime base
	exportStage.Source = p.GetPhaseResult(plan.ExportPhaseBase)
	exportStage.Operations = []plan.Op{
		// Copy installed packages from build stage
		plan.Copy{
			From: plan.Input{Stage: "pip-install"},
			Src:  []string{"/usr/local/lib/python3.13/site-packages"},
			Dest: "/usr/local/lib/python3.13/site-packages",
		},
	}
	exportStage.Provides = []string{"runtime-python-deps"}

	return nil
}
