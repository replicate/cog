package stacks

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/cogpack/stacks/python"
)

var ErrNoStackFound = fmt.Errorf("no stack found that can handle this project")

// SelectStack finds the first stack that can handle the given project
func SelectStack(ctx context.Context, src *project.SourceInfo) (plan.Stack, error) {
	registeredStacks := []plan.Stack{
		&python.PythonStack{},
	}

	for _, stack := range registeredStacks {
		if detected, err := stack.Detect(ctx, src); err != nil {
			return nil, err
		} else if detected {
			return stack, nil
		}
	}
	return nil, ErrNoStackFound
}
