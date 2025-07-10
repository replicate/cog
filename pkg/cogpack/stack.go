package cogpack

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/core"
)

// Stack orchestrates the build for a specific project type (e.g., Python).
// Stacks contain intelligent composition logic and make decisions about
// which blocks to use based on project characteristics.
type Stack interface {
	// Name returns the human-readable name of this stack
	Name() string

	// Detect analyzes the project to determine if this stack can handle it
	Detect(ctx context.Context, src *core.SourceInfo) (bool, error)

	// Plan orchestrates the entire build process for this stack type.
	// This includes block composition, dependency collection/resolution,
	// and plan generation.
	Plan(ctx context.Context, src *core.SourceInfo, plan *Plan) error
}

// PlanResult contains the result of plan generation along with metadata
type PlanResult struct {
	Plan     *Plan             `json:"plan"`     // the generated plan
	Metadata *PlanMetadata     `json:"metadata"` // build context and debug info
	Timing   map[string]string `json:"timing"`   // timing information (future)
}

// PlanMetadata contains build context and debug information
type PlanMetadata struct {
	Stack     string   `json:"stack"`      // e.g., "python"
	Blocks    []string `json:"blocks"`     // active block names
	BaseImage string   `json:"base_image"` // resolved base image
	Version   string   `json:"version"`    // plan schema version
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
func SelectStack(ctx context.Context, src *core.SourceInfo) (Stack, error) {
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
