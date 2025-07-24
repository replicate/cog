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
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/cogpack/plan"
)

// inspectImageConfig inspects an image reference and extracts its environment variables
func inspectImageConfig(ctx context.Context, imageResolver sourceresolver.ImageMetaResolver, imageRef string, platform ocispec.Platform) (*ocispec.ImageConfig, []byte, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse image reference: %w", err)
	}
	named = reference.TagNameOnly(named)

	_, _, blob, err := imageResolver.ResolveImageConfig(ctx, named.String(), sourceresolver.Opt{
		Platform: &platform,
		ImageOpt: &sourceresolver.ResolveImageOpt{
			ResolveMode: llb.ResolveModePreferLocal.String(),
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve image config for %s: %w", imageRef, err)
	}

	var img ocispec.Image
	if err := json.Unmarshal(blob, &img); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal image config: %w", err)
	}

	return &img.Config, blob, nil
}

func resolveInput(ctx context.Context, stageStates map[string]llb.State, platform ocispec.Platform, imageResolver sourceresolver.ImageMetaResolver, in plan.Input) (llb.State, error) {
	switch {
	case in.Stage != "":
		s, ok := stageStates[in.Stage]
		if !ok {
			return llb.State{}, fmt.Errorf("unknown stage %q", in.Stage)
		}
		return s, nil
	case in.Image != "":
		state := llb.Image(in.Image, llb.Platform(platform))

		// fetch image config and apply config to the image state
		_, blob, err := inspectImageConfig(ctx, imageResolver, in.Image, platform)
		if err != nil {
			return llb.State{}, fmt.Errorf("failed to inspect image config: %w", err)
		}
		fmt.Println("IMAGE CONFIG", string(blob))

		state, err = state.WithImageConfig(blob)
		if err != nil {
			return llb.State{}, fmt.Errorf("failed to set image config: %w", err)
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

func union(states ...llb.State) llb.State {
	out := llb.Scratch()
	for _, st := range states {
		out = out.File(llb.Copy(
			st, "/", "/", // src -> dest
			&llb.CopyInfo{
				CopyDirContentsOnly: true, // strip the leading “/”
				CreateDestPath:      true, // mkdir / in the scratch
			}))
	}
	return out
}

// TranslatePlan converts a cogpack.Plan into an LLB State plus stage map.
// If gatewayClient is provided, it will be used to inspect image configs and extract environment variables.
// This is exported for use in tests.
func TranslatePlan(ctx context.Context, p *plan.Plan, imageResolver sourceresolver.ImageMetaResolver) (llb.State, map[string]llb.State, error) {
	stageStates := map[string]llb.State{}

	platform := ocispec.Platform{OS: p.Platform.OS, Architecture: p.Platform.Arch}

	var last llb.State
	hasStage := false

	for _, st := range p.Stages {
		base, err := resolveInput(ctx, stageStates, platform, imageResolver, st.Source)
		if err != nil {
			return llb.State{}, nil, err
		}

		// dir, err := base.GetDir(ctx)
		// if err != nil {
		// 	return llb.State{}, nil, fmt.Errorf("failed to get directory from base state in stage %s: %w", st.ID, err)
		// }
		// fmt.Println("CURRENT DIR FROM BASE", dir)
		// // if dir != "" {
		// // 	base = base.Dir(dir)
		// // }

		// for _, env := range st.Env {
		// 	if eq := strings.Index(env, "="); eq != -1 {
		// 		base = base.AddEnv(env[:eq], env[eq+1:])
		// 	}
		// }

		base, err = applyStageConfigToState(ctx, st, base)
		if err != nil {
			return llb.State{}, nil, fmt.Errorf("failed to apply stage config to state in stage %s: %w", st.ID, err)
		}

		modified, err := applyOps(ctx, base, st, p, stageStates, platform)
		if err != nil {
			return llb.State{}, nil, fmt.Errorf("stage %s: %w", st.ID, err)
		}

		// Try to use diff operation for optimal layer creation
		// Fall back to using the full modified state if diffop is not available (e.g., in dind)
		var final llb.State

		// TODO[md]: Detect diffop availability properly. For now, we'll try diff and handle the error
		// This is a temporary workaround for testing with dind
		useDiff := false // Set to false for dind compatibility

		if useDiff {
			diff := llb.Diff(base, modified)
			final = base.File(llb.Copy(diff, "/", "/"), llb.WithCustomNamef("layer:%s", st.ID))
		} else {
			// Fallback: use the full modified state
			// This creates larger layers but works without diffop
			final = modified.Reset(base)
			// TODO: Log warning about using fallback method
		}

		final, err = copyConfigToState(ctx, modified, final)
		if err != nil {
			return llb.State{}, nil, fmt.Errorf("failed to copy config to state in stage %s: %w", st.ID, err)
		}

		// Apply environment variables from plan
		// TODO: is this what we want!? or do we want the config applied to the beginning state and allow that to carry through?
		for _, env := range st.Env {
			if eq := strings.Index(env, "="); eq != -1 {
				final = final.AddEnv(env[:eq], env[eq+1:])
			}
		}

		if currentPlatform, err := final.GetPlatform(ctx); err == nil && currentPlatform == nil {
			final = final.Platform(platform)
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

func applyStageConfigToState(ctx context.Context, stage *plan.Stage, state llb.State) (llb.State, error) {
	if stage.Dir != "" {
		state = state.Dir(stage.Dir)
	}

	for _, env := range stage.Env {
		if eq := strings.Index(env, "="); eq != -1 {
			state = state.AddEnv(env[:eq], env[eq+1:])
		}
	}

	return state, nil
}

func copyConfigToState(ctx context.Context, from llb.State, to llb.State) (llb.State, error) {
	dir, err := from.GetDir(ctx)
	if err != nil {
		return llb.State{}, fmt.Errorf("failed to get directory from state: %w", err)
	}
	if dir != "" {
		to = to.Dir(dir)
	}

	envList, err := from.Env(ctx)
	if err != nil {
		return llb.State{}, fmt.Errorf("failed to extract environment variables from state: %w", err)
	}

	envArray := envList.ToArray()
	for _, env := range envArray {
		if eq := strings.Index(env, "="); eq != -1 {
			to = to.AddEnv(env[:eq], env[eq+1:])
		}
	}

	platform, err := from.GetPlatform(ctx)
	if err != nil {
		return llb.State{}, fmt.Errorf("failed to get platform from state: %w", err)
	}
	if platform != nil {
		to = to.Platform(*platform)
	}

	return to, nil
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

	copyOpts := &llb.CopyInfo{
		IncludePatterns: copy.Patterns.Include,
		ExcludePatterns: copy.Patterns.Exclude,
		FollowSymlinks:  true,
		// TODO[md]: this should probably be inferred from the path provided (eg if path ends in /, then copyDirContentsOnly should be true)
		CopyDirContentsOnly: true,
		AllowWildcard:       true,
		AllowEmptyWildcard:  true,
		CreateDestPath:      copy.CreateDestPath,
	}

	for _, sp := range copy.Src {
		base = base.File(llb.Copy(src, sp, copy.Dest, copyOpts))
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
