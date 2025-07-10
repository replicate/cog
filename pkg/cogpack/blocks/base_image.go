package blocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack"
	"github.com/replicate/cog/pkg/cogpack/core"
)

// BaseImageBlock establishes the base image for the build
type BaseImageBlock struct{}

// Name returns the human-readable name of this block
func (b *BaseImageBlock) Name() string {
	return "base-image"
}

// Detect determines if this block is needed (always true)
func (b *BaseImageBlock) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return true, nil // Always need a base image
}

// Dependencies returns no dependencies (this block consumes dependencies, doesn't emit them)
func (b *BaseImageBlock) Dependencies(ctx context.Context, src *core.SourceInfo) ([]cogpack.Dependency, error) {
	return nil, nil // This block consumes dependencies from other blocks
}

// Plan establishes the base image stage
func (b *BaseImageBlock) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// Create the base stage that other stages will build from
	stage, err := plan.AddStage(cogpack.PhaseBase, "Base Image", "base")
	if err != nil {
		return err
	}

	// Use the selected base image as input
	stage.Source = cogpack.Input{Image: plan.BaseImage.Build}

	// No operations needed - just establishes the base
	stage.Operations = []cogpack.Op{}

	// Provide base runtime
	stage.Provides = []string{"base-runtime"}

	return nil
}
