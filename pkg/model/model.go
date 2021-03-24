package model

import "time"

type Target string

const (
	TargetDockerCPU = "docker-cpu"
	TargetDockerGPU = "docker-gpu"
)

type Model struct {
	ID           string                  `json:"id"`
	Artifacts    []*Artifact             `json:"artifacts"`
	Config       *Config                 `json:"config"`
	RunArguments map[string]*RunArgument `json:"run_arguments"`
	Created      time.Time               `json:"created"`
}

type Artifact struct {
	Target Target `json:"target"`
	URI    string `json:"uri"`
}

type ArgumentType string

const (
	ArgumentTypeString ArgumentType = "str"
	ArgumentTypeInt    ArgumentType = "int"
	ArgumentTypePath   ArgumentType = "Path"
)

type RunArgument struct {
	Type    ArgumentType `json:"type"`
	Default *string      `json:"default"`
	Help    *string      `json:"help"`
}

func (m *Model) ArtifactFor(target Target) (artifact *Artifact, ok bool) {
	for _, a := range m.Artifacts {
		if a.Target == target {
			return a, true
		}
	}
	return nil, false
}
