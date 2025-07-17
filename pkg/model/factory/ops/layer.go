//go:build ignore

package ops

import (
	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func Layer(name string, ops ...Operation) layerOp {
	return layerOp{
		role: name,
		ops:  ops,
	}
}

type layerOp struct {
	role string
	ops  []Operation
}

func (op layerOp) Apply(ctx types.Context, base llb.State) (llb.State, error) {
	// meta, err := state.GetMeta(ctx, base)
	// if err != nil {
	// 	return llb.State{}, err[]
	// }

	var err error
	intermediate := base

	for _, op := range op.ops {
		intermediate, err = op.Apply(ctx, intermediate)
		if err != nil {
			return llb.State{}, err
		}
	}

	diff := llb.Diff(base, intermediate, llb.WithCustomNamef("layer.diff:%s", op.role))

	// return llb.Merge([]llb.State{base, diff}, llb.WithCustomNamef("merge.layer: %s", op.role)), nil
	// return diff, nil
	final := base.File(
		llb.Copy(diff, "/", "/"),
		llb.WithCustomNamef("layer: %s", op.role),
	)

	// return diff, nil
	// merged := llb.Merge([]llb.State{base, diff})

	// meta.Layers = append(meta.Layers, state.LayerInfo{
	// 	Role: op.role,
	// })

	return final, nil
}
