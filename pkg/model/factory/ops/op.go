package ops

import (
	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

type Operation interface {
	Apply(ctx types.Context, state llb.State) (llb.State, error)
}

type funcOp struct {
	fn func(ctx types.Context, state llb.State) (llb.State, error)
}

func (op funcOp) Apply(ctx types.Context, state llb.State) (llb.State, error) {
	return op.fn(ctx, state)
}

func OpFunc(f func(ctx types.Context, state llb.State) (llb.State, error)) Operation {
	return funcOp{
		fn: f,
	}
}

func Do(ops ...Operation) Operation {
	return doit{
		ops: ops,
	}
}

type doit struct {
	ops []Operation
}

func (op doit) Apply(ctx types.Context, state llb.State) (llb.State, error) {
	var err error
	intermediate := state
	for _, op := range op.ops {
		intermediate, err = op.Apply(ctx, intermediate)
		if err != nil {
			return llb.State{}, err
		}
	}
	return intermediate, nil
}
