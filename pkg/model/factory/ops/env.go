package ops

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

// setEnv sets environment variables
type setEnv struct {
	vars map[string]string
}

func SetEnv(vars map[string]string) *setEnv {
	return &setEnv{vars: vars}
}

func (op *setEnv) Name() string {
	var pairs []string
	for k, v := range op.vars {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return fmt.Sprintf("set-env %s", strings.Join(pairs, " "))
}

func (op *setEnv) ShouldRun(ctx types.Context, state types.State) bool {
	return len(op.vars) > 0
}

func (op *setEnv) Apply(ctx types.Context, state llb.State) (llb.State, error) {
	intermediate := state

	for key, value := range op.vars {
		intermediate = intermediate.AddEnv(key, value)
	}

	return intermediate, nil
}
