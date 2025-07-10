package state

import (
	"errors"

	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func Fork(ctx types.Context, state llb.State) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}

	return state.WithValue(metaKey, meta.Clone()), nil
}

func Merge(ctx types.Context, states ...llb.State) (llb.State, error) {
	if len(states) == 0 {
		return llb.State{}, errors.New("no states to merge")
	}

	lower := states[0]

	if len(states) == 1 {
		return lower, nil
	}

	var metas []*Meta
	var diffs []llb.State
	for idx, s := range states {
		meta, err := GetMeta(ctx, s)
		if err != nil {
			return llb.State{}, err
		}
		metas = append(metas, meta)
		if idx == 0 {
			diffs = append(diffs, s)
		} else {
			diffs = append(diffs, llb.Diff(states[0], s))
		}
	}

	mergedMeta := metas[0].Clone()
	mergedMeta.Merge(metas[1:]...)

	return llb.Merge(diffs).WithValue(metaKey, mergedMeta), nil
}
