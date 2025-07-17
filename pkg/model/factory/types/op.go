//go:build ignore

package types

import "context"

// Operation defines the interface for build operations
type Operation interface {
	// Apply executes the operation on the given state and returns the new state
	Apply(ctx context.Context, buildEnv *BuildEnv, state State) (State, error)

	// Name returns a human-readable name for this operation
	Name() string

	// ShouldRun determines if this operation should execute based on config/state
	ShouldRun(ctx context.Context, buildEnv *BuildEnv, state State) bool
}
