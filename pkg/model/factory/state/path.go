//go:build ignore

package state

import (
	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func PrependPath(ctx types.Context, state llb.State, val string) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}
	meta.path = append([]string{val}, meta.path...)
	return state, nil
}

func AppendPath(ctx types.Context, state llb.State, val string) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}
	meta.path = append(meta.path, val)
	return state, nil
}
