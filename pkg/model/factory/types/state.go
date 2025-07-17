//go:build ignore

package types

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/moby/buildkit/client/llb"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// State represents the current build state including filesystem, environment, and metadata
type State struct {
	LLB llb.State
	// Env    []string
	// Labels map[string]string
	Layers []LayerInfo
	// Config ocispec.ImageConfig

	Cmd        []string
	Entrypoint []string

	Env    map[string]string
	Labels map[string]string
}

func (s *State) Fork() State {
	return State{
		LLB:        s.LLB,
		Layers:     slices.Clone(s.Layers),
		Env:        maps.Clone(s.Env),
		Labels:     maps.Clone(s.Labels),
		Cmd:        slices.Clone(s.Cmd),
		Entrypoint: slices.Clone(s.Entrypoint),
	}
}

func (s *State) MergeLLB(ctx context.Context, llbState llb.State) {
	merged := llb.Merge([]llb.State{s.LLB, llbState})
	s.LLB = merged
}

// SetEnv adds or updates an environment variable
func (s *State) SetEnv(key, value string) {
	if s.Env == nil {
		s.Env = make(map[string]string)
	}
	s.Env[key] = value

	// envVar := key + "=" + value

	// // Remove existing env var with same key
	// for i, env := range s.env {
	// 	if len(env) > len(key) && env[:len(key)+1] == key+"=" {
	// 		s.env[i] = envVar
	// 		return
	// 	}
	// }

	// // Add new env var
	// s.env = append(s.env, envVar)
}

func (s *State) UnsetEnv(key string) {
	delete(s.Env, key)
}

// SetLabel adds or updates a label
func (s *State) SetLabel(key, value string) {
	if s.Labels == nil {
		s.Labels = make(map[string]string)
	}
	s.Labels[key] = value
}

func (s *State) UnsetLabel(key string) {
	delete(s.Labels, key)
}

func (s *State) ToPB(ctx context.Context) (*llb.Definition, error) {
	return s.LLB.Marshal(ctx)
}

func (s *State) ToImage() ocispec.ImageConfig {
	cfg := ocispec.ImageConfig{
		Env:        make([]string, 0, len(s.Env)),
		Labels:     make(map[string]string),
		Cmd:        slices.Clone(s.Cmd),
		Entrypoint: slices.Clone(s.Entrypoint),
	}

	for k, v := range s.Env {
		cfg.Env = append(cfg.Env, fmt.Sprintf("%s=%s", k, v))
	}

	maps.Copy(cfg.Labels, s.Labels)

	return cfg
}

// func StateFromBaseImage(img *docker.ResolvedImage) (State, error) {
// 	env := map[string]string{}

// 	for _, envVar := range img.GetEnvironment() {
// 		k, v, err := parseEnv(envVar)
// 		if err != nil {
// 			return State{}, err
// 		}
// 		env[k] = v
// 	}

// 	state := State{
// 		LLB:    llb.Image(img.Source, llb.Platform(img.Config.Platform)),
// 		Env:    env,
// 		Labels: img.GetLabels(),
// 		Layers: []LayerInfo{
// 			{
// 				Role:        "base",
// 				Description: img.Source,
// 			},
// 		},
// 	}

// 	if len(img.Config.Config.Cmd) > 0 {
// 		state.Cmd = slices.Clone(img.Config.Config.Cmd)
// 	}
// 	if len(img.Config.Config.Entrypoint) > 0 {
// 		state.Entrypoint = slices.Clone(img.Config.Config.Entrypoint)
// 	}

// 	return state, nil
// }

func parseEnv(kv string) (string, string, error) {
	parts := strings.SplitN(kv, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid env var: %s", kv)
	}
	return parts[0], parts[1], nil
}

// LayerInfo tracks information about each layer added to the image
type LayerInfo struct {
	Role        string // e.g., "base", "sys-deps", "model", "weights"
	Description string
	Size        int64
}
