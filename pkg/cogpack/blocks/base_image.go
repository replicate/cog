package blocks

import (
	"context"

	p "github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// BaseImageBlock establishes the base image for the build
type BaseImageBlock struct{}

// Name returns the human-readable name of this block
func (b *BaseImageBlock) Name() string {
	return "base-image"
}

// Detect determines if this block is needed (always true)
func (b *BaseImageBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return true, nil // Always need a base image
}

// Dependencies returns no dependencies (this block consumes dependencies, doesn't emit them)
func (b *BaseImageBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]p.Dependency, error) {
	return nil, nil // This block consumes dependencies from other blocks
}

// Plan establishes both build and export base image stages
func (b *BaseImageBlock) Plan(ctx context.Context, src *project.SourceInfo, plan *p.Plan) error {
	// Create the build base stage
	buildStage, err := plan.AddStage(p.PhaseBase, "Build Base", "build-base")
	if err != nil {
		return err
	}

	// Use the build base image
	buildStage.Source = p.Input{Image: plan.BaseImage.Build}
	buildStage.Operations = []p.Op{} // No operations needed for base
	buildStage.Provides = []string{"build-base"}

	// Create the export base stage for runtime image
	exportStage, err := plan.AddStage(p.ExportPhaseBase, "Runtime Base", "runtime-base")
	if err != nil {
		return err
	}

	// Use the runtime base image
	exportStage.Source = p.Input{Image: plan.BaseImage.Runtime}
	exportStage.Operations = []p.Op{} // r8.im images already have what we need
	exportStage.Provides = []string{"runtime-base"}

	return nil
}
