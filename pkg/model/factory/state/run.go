package state

import (
	"strings"

	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func Run(ctx types.Context, state llb.State, opts ...llb.RunOption) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}

	path := strings.Join(meta.path, ":")
	opts = append(opts, llb.AddEnv("PATH", path))

	for k, v := range meta.Env {
		opts = append(opts, llb.AddEnv(k, v))
	}

	return state.Run(opts...).Root(), nil
}
