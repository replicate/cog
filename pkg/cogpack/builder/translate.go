package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/moby/buildkit/frontend/gateway/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/cogpack/plan"
)

// inspectImageConfig inspects an image reference and extracts its environment variables
func inspectImageConfig(ctx context.Context, gatewayClient client.Client, imageRef string, platform ocispec.Platform) ([]string, error) {
	if gatewayClient == nil {
		return nil, nil // Skip inspection if no gateway client
	}

	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference: %w", err)
	}
	named = reference.TagNameOnly(named)

	_, _, blob, err := gatewayClient.ResolveImageConfig(ctx, named.String(), sourceresolver.Opt{
		Platform: &platform,
		ImageOpt: &sourceresolver.ResolveImageOpt{
			ResolveMode: llb.ResolveModePreferLocal.String(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve image config for %s: %w", imageRef, err)
	}

	var img ocispec.Image
	if err := json.Unmarshal(blob, &img); err != nil {
		return nil, fmt.Errorf("failed to unmarshal image config: %w", err)
	}

	return img.Config.Env, nil
}

// translatePlan converts a cogpack.Plan into an LLB State plus stage map.
// If gatewayClient is provided, it will be used to inspect image configs and extract environment variables.
func translatePlan(ctx context.Context, p *plan.Plan, gatewayClient client.Client) (llb.State, map[string]llb.State, error) {
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
			state := llb.Image(in.Image, llb.Platform(platform))
			
			// Extract environment variables from image config if gateway client is available
			if gatewayClient != nil {
				envVars, err := inspectImageConfig(ctx, gatewayClient, in.Image, platform)
				if err != nil {
					// Log the error but don't fail the build - environment variables are nice to have
					fmt.Printf("Warning: failed to inspect image config for %s: %v\n", in.Image, err)
				} else {
					// Apply environment variables from image config to the LLB state
					for _, env := range envVars {
						if eq := strings.Index(env, "="); eq != -1 {
							state = state.AddEnv(env[:eq], env[eq+1:])
						}
					}
				}
			}
			
			return state, nil
		case in.Local != "":
			return llb.Local(in.Local), nil
		case in.URL != "":
			return llb.HTTP(in.URL), nil
		case in.Scratch:
			return llb.Scratch(), nil
		default:
			return llb.State{}, fmt.Errorf("invalid input: %v", in)
		}
	}

	stages := append([]*plan.Stage{}, p.BuildStages...)
	stages = append(stages, p.ExportStages...)

	var last llb.State
	hasStage := false

	for _, st := range stages {
		base, err := resolveInput(st.Source)
		if err != nil {
			return llb.State{}, nil, err
		}

		for _, env := range st.Env {
			if eq := strings.Index(env, "="); eq != -1 {
				base = base.AddEnv(env[:eq], env[eq+1:])
			}
		}

		modified, err := applyOps(ctx, base, st, p, stageStates, platform)
		if err != nil {
			return llb.State{}, nil, fmt.Errorf("stage %s: %w", st.ID, err)
		}

		diff := llb.Diff(base, modified)
		final := base.File(llb.Copy(diff, "/", "/"), llb.WithCustomNamef("layer:%s", st.ID))
		
		// Preserve environment variables from the modified state
		// Re-apply stage env vars to the final state since they were lost in the diff operation
		for _, env := range st.Env {
			if eq := strings.Index(env, "="); eq != -1 {
				final = final.AddEnv(env[:eq], env[eq+1:])
			}
		}

		stageStates[st.ID] = final
		last = final
		hasStage = true
	}

	if !hasStage {
		return llb.State{}, nil, fmt.Errorf("plan contained no stages")
	}

	return last, stageStates, nil
}

func applyOps(ctx context.Context, base llb.State, st *plan.Stage, p *plan.Plan, stageStates map[string]llb.State, platform ocispec.Platform) (llb.State, error) {
	cur := base
	for _, op := range st.Operations {
		var err error
		switch o := op.(type) {
		case plan.Exec:
			cur, err = applyExecOp(ctx, cur, o, st, p, stageStates, platform)
		case plan.Copy:
			cur, err = applyCopyOp(ctx, cur, o, p, stageStates, platform)
		case plan.Add:
			cur, err = applyAddOp(ctx, cur, o, p, stageStates, platform)
		case plan.SetEnv:
			cur, err = applySetEnvOp(ctx, cur, o)
		case plan.MkFile:
			cur, err = applyMkFileOp(ctx, cur, o)
		default:
			return llb.State{}, fmt.Errorf("unsupported op %T", o)
		}
		if err != nil {
			return llb.State{}, err
		}
	}
	return cur, nil
}

// applyMounts converts plan.Mount structs to BuildKit LLB mount options
func applyMounts(mounts []plan.Mount, p *plan.Plan, stageStates map[string]llb.State, platform ocispec.Platform) ([]llb.RunOption, error) {
	var opts []llb.RunOption

	for _, mount := range mounts {
		// Validate mount input
		if err := validateMountInput(mount.Source, p, stageStates); err != nil {
			return nil, fmt.Errorf("invalid mount source: %w", err)
		}

		source, err := resolveMountInput(mount.Source, p, stageStates, platform)
		if err != nil {
			return nil, fmt.Errorf("resolve mount source: %w", err)
		}

		opts = append(opts, llb.AddMount(mount.Target, source))
	}

	return opts, nil
}

// validateMountInput validates that a mount input is correct
func validateMountInput(input plan.Input, p *plan.Plan, stageStates map[string]llb.State) error {
	// Check that exactly one input type is specified
	inputCount := 0
	if input.Phase != "" {
		inputCount++
	}
	if input.Stage != "" {
		inputCount++
	}
	if input.Image != "" {
		inputCount++
	}
	if input.Local != "" {
		inputCount++
	}

	if inputCount == 0 {
		return fmt.Errorf("mount input must specify phase, stage, image, or local")
	}

	if inputCount > 1 {
		return fmt.Errorf("mount input must specify exactly one of phase, stage, image, or local")
	}

	// Validate phase reference exists and has stages
	if input.Phase != "" {
		return fmt.Errorf("phase input not supported")
		// phaseResult := p.GetPhaseResult(input.Phase)
		// if phaseResult.Stage == "" {
		// 	return fmt.Errorf("phase %q has no stages", input.Phase)
		// }
		// if _, ok := stageStates[phaseResult.Stage]; !ok {
		// 	return fmt.Errorf("phase %q result stage %q does not exist", input.Phase, phaseResult.Stage)
		// }
	}

	// Validate stage reference exists
	if input.Stage != "" {
		if _, ok := stageStates[input.Stage]; !ok {
			return fmt.Errorf("stage %q does not exist", input.Stage)
		}
	}

	// Image and Local validations are implicit since we already checked they're not empty strings above

	return nil
}

// resolveMountInput resolves a plan.Input to an LLB state for mount operations
func resolveMountInput(in plan.Input, p *plan.Plan, stageStates map[string]llb.State, platform ocispec.Platform) (llb.State, error) {
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
	case in.URL != "":
		return llb.HTTP(in.URL), nil
	case in.Scratch:
		return llb.Scratch(), nil
	default:
		return llb.State{}, fmt.Errorf("invalid mount input: %v", in)
	}
}

// applyExecOp handles Exec operations with mount support
func applyExecOp(ctx context.Context, base llb.State, exec plan.Exec, st *plan.Stage, p *plan.Plan, stageStates map[string]llb.State, platform ocispec.Platform) (llb.State, error) {
	opts := []llb.RunOption{llb.Shlex(exec.Command)}
	if st.Dir != "" {
		opts = append(opts, llb.Dir(st.Dir))
	}

	// Apply mounts
	mountOpts, err := applyMounts(exec.Mounts, p, stageStates, platform)
	if err != nil {
		return llb.State{}, err
	}
	opts = append(opts, mountOpts...)

	return base.Run(opts...).Root(), nil
}

// applyCopyOp handles Copy operations
func applyCopyOp(ctx context.Context, base llb.State, copy plan.Copy, p *plan.Plan, stageStates map[string]llb.State, platform ocispec.Platform) (llb.State, error) {
	src, err := resolveMountInput(copy.From, p, stageStates, platform)
	if err != nil {
		return llb.State{}, fmt.Errorf("resolve copy source: %w", err)
	}

	for _, sp := range copy.Src {
		base = base.File(llb.Copy(src, sp, copy.Dest))
	}
	return base, nil
}

// applyAddOp handles Add operations
func applyAddOp(ctx context.Context, base llb.State, add plan.Add, p *plan.Plan, stageStates map[string]llb.State, platform ocispec.Platform) (llb.State, error) {
	if add.From.Stage != "" || add.From.Image != "" || add.From.Local != "" || add.From.URL != "" {
		// Add with From source - copy from source first
		src, err := resolveMountInput(add.From, p, stageStates, platform)
		if err != nil {
			return llb.State{}, fmt.Errorf("resolve add source: %w", err)
		}

		for _, sp := range add.Src {
			base = base.File(llb.Copy(src, sp, add.Dest))
		}
		return base, nil
	}

	// Traditional Add with URLs in Src
	for _, sp := range add.Src {
		base = base.File(llb.Copy(llb.HTTP(sp), "download.bin", add.Dest))
	}
	return base, nil
}

// applySetEnvOp handles SetEnv operations
func applySetEnvOp(ctx context.Context, base llb.State, setEnv plan.SetEnv) (llb.State, error) {
	for k, v := range setEnv.Vars {
		base = base.AddEnv(k, v)
	}
	return base, nil
}

// applyMkFileOp handles MkFile operations
func applyMkFileOp(ctx context.Context, base llb.State, mkFile plan.MkFile) (llb.State, error) {
	return base.File(llb.Mkfile(mkFile.Dest, os.FileMode(mkFile.Mode), mkFile.Data)), nil
}
