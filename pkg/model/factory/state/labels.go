//go:build ignore

package state

import (
	"fmt"

	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func SetLabel(ctx types.Context, state llb.State, k, v string) (llb.State, error) {
	fmt.Println("SetLabel", k, v)
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels[k] = v
	return state, nil
}
