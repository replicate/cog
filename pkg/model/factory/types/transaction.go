package types

import (
	"context"

	"github.com/moby/buildkit/client/llb"
)

// Transaction represents a group of operations that can be squashed together
type Transaction struct {
	operations []Operation
	role       string
	name       string
}

// NewTransaction creates a new transaction with the given role
func NewTransaction(role, name string) *Transaction {
	return &Transaction{
		role: role,
		name: name,
	}
}

// Add adds an operation to the transaction
func (t *Transaction) Add(op Operation) *Transaction {
	t.operations = append(t.operations, op)
	return t
}

// Apply executes all operations in the transaction and squashes the result
func (t *Transaction) Apply(ctx context.Context, buildEnv *BuildEnv, baseState State) (State, error) {
	if len(t.operations) == 0 {
		return baseState, nil
	}

	// Start with base state
	currentState := baseState

	// Apply all operations
	for _, op := range t.operations {
		if !op.ShouldRun(ctx, buildEnv, currentState) {
			continue
		}

		newState, err := op.Apply(ctx, buildEnv, currentState)
		if err != nil {
			return State{}, err
		}
		currentState = newState
	}

	// Squash the result back to base
	squashedLLB, err := squash(baseState.LLB, currentState.LLB)
	if err != nil {
		return State{}, err
	}

	// Create final state with squashed filesystem but accumulated metadata
	finalState := State{
		LLB:    squashedLLB,
		Env:    currentState.Env,
		Labels: currentState.Labels,
		Layers: append(baseState.Layers, LayerInfo{
			Role:        t.role,
			Description: t.name,
		}),
	}

	return finalState, nil
}

// squash combines two LLB states using diff/copy pattern
func squash(base, target llb.State) (llb.State, error) {
	diff := llb.Diff(base, target)
	return base.File(llb.Copy(diff, "/", "/")), nil
}
