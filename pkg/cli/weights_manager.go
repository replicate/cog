package cli

import (
	"errors"
	"fmt"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/weights"
)

// weightRegistryResolutionHelp is shared help text for weight
// subcommands so the docs can't drift between import/pull/status.
const weightRegistryResolutionHelp = "The registry is determined from the resolved model ref ('model' in\n" +
	"cog.yaml, optionally overridden by COG_MODEL or COG_MODEL_REPO)."

func newWeightManager(src *model.Source) (*weights.Manager, error) {
	repo, err := resolveWeightRepo(src, configFilename)
	if err != nil {
		return nil, err
	}
	return weights.NewFromSource(src, repo)
}

// resolveWeightRepo returns the bundle repository (registry/repo, no
// tag) for weight operations. Returns ("", nil) for weight-less
// projects — preserves the no-op Manager contract used by cog predict
// and cog train. cfgPath is woven into user-facing errors so messages
// reflect the actual --config flag value, not a hardcoded "cog.yaml".
//
// The image-in-cog.yaml branch is defense-in-depth: config validation
// already rejects 'image:' + 'weights:'; this catches programmatic
// callers that build a Source from a hand-rolled Config.
func resolveWeightRepo(src *model.Source, cfgPath string) (string, error) {
	if len(src.Config.Weights) == 0 {
		return "", nil
	}
	if src.Config.Image != "" {
		return "", fmt.Errorf("weight commands require 'model' in %s, not 'image' — rename 'image' to 'model'", cfgPath)
	}
	ref, err := model.ResolveModelRef("", src.Config.Model)
	if err != nil {
		if errors.Is(err, model.ErrNoModelRef) {
			return "", fmt.Errorf("weight commands require a model ref: set 'model' in %s, or export COG_MODEL / COG_MODEL_REPO", cfgPath)
		}
		return "", fmt.Errorf("resolving model ref: %w", err)
	}
	return ref.Repository(), nil
}
