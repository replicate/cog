package python

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// PythonVersionBlock emits Python version dependency from cog.yaml
type PythonVersionBlock struct{}

// Name returns the human-readable name of this block
func (b *PythonVersionBlock) Name() string {
	return "python-version"
}

// Detect determines if this block is needed (always true for Python projects)
func (b *PythonVersionBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return true, nil // Always need Python version for Python projects
}

// Dependencies emits Python version dependency from cog.yaml
func (b *PythonVersionBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error) {
	// Get Python version from cog.yaml, default to 3.11 if not specified
	pythonVersion := "3.11"
	if src.Config.Build.PythonVersion != "" {
		pythonVersion = src.Config.Build.PythonVersion
	}

	return []plan.Dependency{
		{
			Name:             "python",
			Provider:         "python-version",
			RequestedVersion: pythonVersion,
			Source:           "cog.yaml",
		},
	}, nil
}

// Plan doesn't create any stages - this block only emits dependencies
func (b *PythonVersionBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// This block only emits dependencies, no stages needed
	return nil
}
