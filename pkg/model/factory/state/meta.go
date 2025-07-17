//go:build ignore

package state

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/model/factory/types"
)

func MetaFromImage(img *ocispec.Image) *Meta {
	meta := &Meta{
		User:         img.Config.User,
		WorkingDir:   img.Config.WorkingDir,
		Entrypoint:   slices.Clone(img.Config.Entrypoint),
		Cmd:          slices.Clone(img.Config.Cmd),
		Labels:       maps.Clone(img.Config.Labels),
		ExposedPorts: maps.Clone(img.Config.ExposedPorts),
		Env:          map[string]string{},
	}
	meta.SetEnviron(img.Config.Env)

	return meta
}

type Meta struct {
	// BaseImage *ocispec.Image

	User         string
	Cmd          []string
	Entrypoint   []string
	Env          map[string]string
	ExposedPorts map[string]struct{}
	Labels       map[string]string
	Layers       []LayerInfo
	path         []string
	WorkingDir   string
}

func (m *Meta) SetEnviron(env []string) {
	fmt.Println("set environ", env)
	for _, envVar := range env {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}
		m.SetEnv(parts[0], parts[1])
	}
}

func (m *Meta) SetEnv(k, v string) {
	fmt.Println("set env", k, v)
	if k == "PATH" {
		m.SetPath(v)
		return
	}
	if m.Env == nil {
		m.Env = map[string]string{}
	}
	m.Env[k] = v
}

func (m *Meta) UnsetEnv(k string) {
	if k == "PATH" {
		m.path = []string{}
		return
	}
	delete(m.Env, k)
}

func (m *Meta) GetEnv() []string {
	env := []string{}
	for k, v := range m.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	env = append(env, fmt.Sprintf("PATH=%s", strings.Join(m.path, ":")))
	return env
}

func (m *Meta) SetPath(path string) {
	m.path = mergePaths(m.path, strings.Split(path, ":"))
}

func (m *Meta) AppendPath(path string) {
	for _, part := range strings.Split(path, ":") {
		// ignore $PATH since we're appending
		if part == "$PATH" {
			continue
		} else {
			m.path = append(m.path, part)
		}
	}
}

func (m *Meta) PrependPath(path string) {
	m.path = mergePaths(m.path, strings.Split(path, ":"))
}

func mergePaths(base []string, incoming []string) []string {
	if !slices.Contains(incoming, "$PATH") {
		return incoming
	}

	var newPath []string
	var baseApplied bool
	for _, part := range incoming {
		if part == "$PATH" && !baseApplied && base != nil {
			newPath = append(newPath, base...)
			baseApplied = true
			continue
		}
		newPath = append(newPath, part)
	}

	return newPath
}

func (m *Meta) UnsetPath() {
	m.path = []string{}
}

func (m *Meta) ExposePort(port string) {
	fmt.Println("set exposed port")
	if m.ExposedPorts == nil {
		m.ExposedPorts = map[string]struct{}{}
	}
	m.ExposedPorts[port] = struct{}{}
}

func (m *Meta) UnexposePort(port string) {
	fmt.Println("unset exposed port")
	delete(m.ExposedPorts, port)
}

func (m *Meta) Clone() *Meta {
	return &Meta{
		User:         m.User,
		Cmd:          slices.Clone(m.Cmd),
		Entrypoint:   slices.Clone(m.Entrypoint),
		Env:          maps.Clone(m.Env),
		ExposedPorts: maps.Clone(m.ExposedPorts),
		Labels:       maps.Clone(m.Labels),
		Layers:       slices.Clone(m.Layers),
		path:         slices.Clone(m.path),
		WorkingDir:   m.WorkingDir,
	}
}

func (m *Meta) Merge(others ...*Meta) {
	for _, other := range others {
		m.User = other.User
		m.Cmd = slices.Clone(other.Cmd)
		m.Entrypoint = slices.Clone(other.Entrypoint)
		maps.Copy(m.ExposedPorts, other.ExposedPorts)
		for k, v := range other.Env {
			m.SetEnv(k, v)
		}
		maps.Copy(m.Env, other.Env)
		m.path = slices.Clone(other.path)
		m.WorkingDir = other.WorkingDir
		// this is almost certainly wrong
		m.Layers = append(m.Layers, other.Layers...)
	}
}

func (m *Meta) ToImageConfig() ocispec.ImageConfig {
	return ocispec.ImageConfig{
		User:         m.User,
		Cmd:          m.Cmd,
		Entrypoint:   m.Entrypoint,
		Env:          m.GetEnv(),
		ExposedPorts: m.ExposedPorts,
		Labels:       m.Labels,
		WorkingDir:   m.WorkingDir,
	}
}

type LayerInfo struct {
	Digest digest.Digest
	Role   string
}

var metaKey = struct{}{}

// func initMeta(ctx context.Context, state llb.State) (llb.State, error) {
// 	val, err := state.Value(ctx, metaKey)
// 	if err != nil {
// 		return state, err
// 	}

// 	meta := val.(*Meta)
// 	return state.WithValue(metaKey, meta), nil
// }

func WithMeta(state llb.State, meta *Meta) llb.State {
	return state.WithValue(metaKey, meta)
}

func GetMeta(ctx types.Context, state llb.State) (*Meta, error) {
	val, err := state.Value(ctx, metaKey)
	if err != nil {
		return nil, err
	}
	return val.(*Meta), nil
}

type MetaOption func(state llb.State, meta *Meta) llb.State

func WithWorkingDir(dir string) MetaOption {
	return func(state llb.State, meta *Meta) llb.State {
		meta.WorkingDir = dir
		return state.Dir(dir)
	}
}

func WithExposedPort(port string) MetaOption {
	return func(state llb.State, meta *Meta) llb.State {
		meta.ExposePort(port)
		return state
	}
}

func WithEntrypoint(entrypoint []string) MetaOption {
	return func(state llb.State, meta *Meta) llb.State {
		meta.Entrypoint = entrypoint
		return state
	}
}

func WithCmd(cmd []string) MetaOption {
	return func(state llb.State, meta *Meta) llb.State {
		meta.Cmd = cmd
		return state
	}
}

func WithConfig(ctx types.Context, state llb.State, opts ...MetaOption) (llb.State, error) {
	meta, err := GetMeta(ctx, state)
	if err != nil {
		return state, err
	}

	for _, opt := range opts {
		state = opt(state, meta)
	}

	return state, nil
}

// func InitMeta(state llb.State) (llb.State, error ) {
// 	if state.V

// 	meta := val.(*Meta)

// 	return state.WithValue(metaKey, meta)
// }
