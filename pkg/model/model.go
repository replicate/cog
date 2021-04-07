package model

import "time"

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
	Target string `json:"target"`
	URI    string `json:"uri"`
}

type ArgumentType string

const (
	ArgumentTypeString ArgumentType = "str"
	ArgumentTypeInt    ArgumentType = "int"
	ArgumentTypeFloat  ArgumentType = "float"
	ArgumentTypeBool   ArgumentType = "bool"
	ArgumentTypePath   ArgumentType = "Path"
)

type RunArgument struct {
	Type    ArgumentType `json:"type"`
	Default *string      `json:"default"`
	Min     *string      `json:"min"`
	Max     *string      `json:"max"`
	Help    *string      `json:"help"`
}

func (m *Model) ArtifactFor(target string) (artifact *Artifact, ok bool) {
	for _, a := range m.Artifacts {
		if a.Target == target {
			return a, true
		}
	}
	return nil, false
}
