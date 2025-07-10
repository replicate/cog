package blocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// Block is a self-contained build component (e.g., install apt packages, manage Python dependencies).
// Blocks are focused on implementation details while staying decoupled from other blocks.
type Block interface {
	// Name returns the human-readable name of this block
	Name() string

	// Detect analyzes the project to determine if this block is needed
	Detect(ctx context.Context, src *project.SourceInfo) (bool, error)

	// Dependencies returns the dependency requirements this block is responsible for.
	// These will be collected and resolved centrally before plan generation.
	Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error)

	// Plan contributes build operations to the overall Plan.
	// This is called after dependencies have been resolved, so blocks can
	// access resolved versions via plan.Dependencies.
	Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error
}
