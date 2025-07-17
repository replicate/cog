//go:build ignore

package factory

import (
	"context"

	"github.com/moby/buildkit/frontend/gateway/client"

	"github.com/replicate/cog/pkg/model/factory/types"
)

// Builder orchestrates the build process using operations and transactions
type Builder struct {
	buildEnv *types.BuildEnv
	state    types.State
}

// NewBuilder creates a new builder with the given context and initial state
func NewBuilder(buildEnv *types.BuildEnv, initialState types.State) *Builder {
	return &Builder{
		buildEnv: buildEnv,
		state:    initialState,
	}
}

func (b *Builder) Solve(ctx context.Context) (*client.Result, error) {
	return nil, nil
}

// Apply executes a single operation
func (b *Builder) Apply(ctx context.Context, op types.Operation) error {
	if !op.ShouldRun(ctx, b.buildEnv, b.state) {
		return nil
	}

	newState, err := op.Apply(ctx, b.buildEnv, b.state)
	if err != nil {
		return err
	}

	b.state = newState
	return nil
}

// Transaction executes a transaction and updates the builder state
func (b *Builder) Transaction(ctx context.Context, tx *types.Transaction) error {
	newState, err := tx.Apply(ctx, b.buildEnv, b.state)
	if err != nil {
		return err
	}

	b.state = newState
	return nil
}

// State returns the current build state
func (b *Builder) State() types.State {
	return b.state
}
