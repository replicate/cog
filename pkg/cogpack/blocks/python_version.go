package blocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack"
	"github.com/replicate/cog/pkg/cogpack/core"
)

// PythonVersionBlock reads Python version from cog.yaml and emits a dependency
type PythonVersionBlock struct{}

// Name returns the human-readable name of this block
func (b *PythonVersionBlock) Name() string {
	return "python-version"
}

// Detect determines if this block is needed (always true for Python projects)
func (b *PythonVersionBlock) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return true, nil // Always active for Python projects
}

// Dependencies returns the Python version dependency
func (b *PythonVersionBlock) Dependencies(ctx context.Context, src *core.SourceInfo) ([]cogpack.Dependency, error) {
	version := src.Config.Build.PythonVersion
	source := "default"

	if version == "" {
		version = "3.11" // default version
	} else {
		source = "cog.yaml"
	}

	return []cogpack.Dependency{{
		Name:             "python",
		Provider:         b.Name(),
		RequestedVersion: version,
		Source:           source,
	}}, nil
}

// Plan contributes to the build plan (this block doesn't emit build steps, just dependencies)
func (b *PythonVersionBlock) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// This block only emits dependencies, no build steps
	return nil
}
