package builder

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/buildkit/client/llb"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/cogpack/plan"
)

// translatePlan converts a cogpack.Plan into an LLB State plus stage map.
func translatePlan(ctx context.Context, p *plan.Plan) (llb.State, map[string]llb.State, error) {
	stageStates := map[string]llb.State{}

	platform := ocispec.Platform{OS: p.Platform.OS, Architecture: p.Platform.Arch}

	resolveInput := func(in plan.Input) (llb.State, error) {
		switch {
		case in.Stage != "":
			s, ok := stageStates[in.Stage]
			if !ok {
				return llb.State{}, fmt.Errorf("unknown stage %q", in.Stage)
			}
			return s, nil
		case in.Image != "":
			return llb.Image(in.Image, llb.Platform(platform)), nil
		case in.Local != "":
			return llb.Local(in.Local), nil
		default:
			return llb.Scratch(), nil
		}
	}

	phases := append([]*plan.Phase{}, p.BuildPhases...)
	phases = append(phases, p.ExportPhases...)

	var last llb.State
	hasStage := false

	for _, ph := range phases {
		for _, st := range ph.Stages {
			base, err := resolveInput(st.Source)
			if err != nil {
				return llb.State{}, nil, err
			}

			for _, env := range st.Env {
				if eq := strings.Index(env, "="); eq != -1 {
					base = base.AddEnv(env[:eq], env[eq+1:])
				}
			}

			modified, err := applyOps(ctx, base, st, stageStates, platform)
			if err != nil {
				return llb.State{}, nil, fmt.Errorf("stage %s: %w", st.ID, err)
			}

			diff := llb.Diff(base, modified)
			final := base.File(llb.Copy(diff, "/", "/"), llb.WithCustomNamef("layer:%s", st.ID))

			stageStates[st.ID] = final
			last = final
			hasStage = true
		}
	}

	if !hasStage {
		return llb.State{}, nil, fmt.Errorf("plan contained no stages")
	}

	return last, stageStates, nil
}

func applyOps(ctx context.Context, base llb.State, st *plan.Stage, stageStates map[string]llb.State, platform ocispec.Platform) (llb.State, error) {
	cur := base
	for _, op := range st.Operations {
		switch o := op.(type) {
		case plan.Exec:
			opts := []llb.RunOption{llb.Shlex(o.Command)}
			if st.Dir != "" {
				opts = append(opts, llb.Dir(st.Dir))
			}
			cur = cur.Run(opts...).Root()

		case plan.Copy:
			var src llb.State
			if o.From != "" {
				if s, ok := stageStates[o.From]; ok {
					src = s
				} else if strings.HasPrefix(o.From, "local:") {
					src = llb.Local(strings.TrimPrefix(o.From, "local:"))
				} else {
					src = llb.Image(o.From, llb.Platform(platform))
				}
			} else {
				src = llb.Scratch()
			}
			for _, sp := range o.Src {
				cur = cur.File(llb.Copy(src, sp, o.Dest))
			}

		case plan.Add:
			for _, sp := range o.Src {
				cur = cur.File(llb.Copy(llb.HTTP(sp), "download.bin", o.Dest))
			}

		case plan.SetEnv:
			for k, v := range o.Vars {
				cur = cur.AddEnv(k, v)
			}

		default:
			return llb.State{}, fmt.Errorf("unsupported op %T", o)
		}
	}
	return cur, nil
}
