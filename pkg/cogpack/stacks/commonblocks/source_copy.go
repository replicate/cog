package commonblocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// SourceCopyBlock copies the model source code into the runtime image
type SourceCopyBlock struct{}

// Name returns the human-readable name of this block
func (b *SourceCopyBlock) Name() string {
	return "source-copy"
}

// Detect determines if this block is needed (always true for Python projects)
func (b *SourceCopyBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return true, nil // Always copy source for Python models
}

// Dependencies returns no dependencies
func (b *SourceCopyBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error) {
	return nil, nil
}

// Plan copies the source code into the runtime image
func (b *SourceCopyBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// Create export stage to copy source to runtime image
	stage, err := p.AddStage(plan.ExportPhaseApp, "Copy Source", "copy-source")
	if err != nil {
		return err
	}

	// Build from the runtime base
	stage.Source = p.GetPhaseResult(plan.ExportPhaseBase)

	// Copy source files to /src directory
	stage.Dir = "/src"
	stage.Operations = []plan.Op{
		// Copy source files from build context
		plan.Copy{
			From: "local:context",
			Src:  []string{"."},
			Dest: "/src",
			Patterns: plan.FilePattern{
				Exclude: []string{
					".cog",
					"__pycache__",
					"*.pyc",
					".git",
					".gitignore",
					"*.md",
				},
			},
		},
	}

	// Set the final export configuration for the runtime image
	p.Export = &plan.ExportConfig{
		Entrypoint: []string{"python", "-m", "cog.server.http"},
		WorkingDir: "/src",
		Labels: map[string]string{
			"org.cogmodel.config_schema": "1.0",
			"org.cogmodel.cog_version":   "0.9.0",
		},
	}

	stage.Provides = []string{"model-source"}

	return nil
}
