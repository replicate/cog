package stacks

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/plan"
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
	Plan(ctx context.Context, src *project.SourceInfo, plan *plan.Plan) error
}

// registeredStacks holds the global stack registry
var registeredStacks []Stack

// RegisterStack adds a stack to the global registry
func RegisterStack(stack Stack) {
	registeredStacks = append(registeredStacks, stack)
}

// GetRegisteredStacks returns a copy of the registered stacks
func GetRegisteredStacks() []Stack {
	stacks := make([]Stack, len(registeredStacks))
	copy(stacks, registeredStacks)
	return stacks
}

// SelectStack finds the first stack that can handle the given project
func SelectStack(ctx context.Context, src *project.SourceInfo) (Stack, error) {
	for _, stack := range registeredStacks {
		if detected, err := stack.Detect(ctx, src); err != nil {
			return nil, err
		} else if detected {
			return stack, nil
		}
	}
	return nil, ErrNoStackFound
}

// ErrNoStackFound is returned when no stack can handle the project
var ErrNoStackFound = fmt.Errorf("no stack found that can handle this project")
