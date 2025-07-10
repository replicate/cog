package state

import (
	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func SetEnvs(ctx types.Context, state llb.State, env map[string]string) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}
	for k, v := range env {
		meta.Env[k] = v
	}
	return state, nil
}

func SetEnv(ctx types.Context, state llb.State, k, v string) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}
	meta.Env[k] = v
	return state, nil
}

func UnsetEnv(ctx types.Context, state llb.State, k string) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return llb.State{}, err
	}
	delete(meta.Env, k)
	return state, nil
}

func GetEnv(ctx types.Context, state llb.State) ([]string, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return nil, err
	}
	return meta.GetEnv(), nil
}
