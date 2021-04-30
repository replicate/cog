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
	Stats        *Stats                  `json:"stats"`
}

type Stats struct {
	BootTime    float64 `json:"boot_time"`
	SetupTime   float64 `json:"setup_time"`
	RunTime     float64 `json:"run_time"`
	MemoryUsage uint64  `json:"memory_usage"`
	CPUUsage    float64 `json:"cpu_usage"`
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
	Options *[]string    `json:"options"`
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
