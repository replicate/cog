package plan

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/project"
)

// Stack orchestrates the build for a specific project type (e.g., Python).
// Stacks contain intelligent composition logic and make decisions about
// which blocks to use based on project characteristics.
type Stack interface {
	// Name returns the human-readable name of this stack
	Name() string

	// Detect analyzes the project to determine if this stack can handle it
	Detect(ctx context.Context, src *project.SourceInfo) (bool, error)

	// Plan orchestrates the entire build process for this stack type.
	// This includes block composition, dependency collection/resolution,
	// and plan generation.
	Plan(ctx context.Context, src *project.SourceInfo, composer *PlanComposer) error
}
