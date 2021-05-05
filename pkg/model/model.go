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
	BootTimeCPU    float64 `json:"boot_time"`
	SetupTimeCPU   float64 `json:"setup_time"`
	RunTimeCPU     float64 `json:"run_time"`
	MemoryUsageCPU uint64  `json:"memory_usage"`
	CPUUsageCPU    float64 `json:"cpu_usage"`
	BootTimeGPU    float64 `json:"boot_time_gpu"`
	SetupTimeGPU   float64 `json:"setup_time_gpu"`
	RunTimeGPU     float64 `json:"run_time_gpu"`
	MemoryUsageGPU uint64  `json:"memory_usage_gpu"`
	CPUUsageGPU    float64 `json:"cpu_usage_gpu"`
}

func (s *Stats) SetBootTime(bootTime float64, gpu bool) {
	if gpu {
		s.BootTimeGPU = bootTime
	} else {
		s.BootTimeCPU = bootTime
	}
}

func (s *Stats) SetSetupTime(setupTime float64, gpu bool) {
	if gpu {
		s.SetupTimeGPU = setupTime
	} else {
		s.SetupTimeCPU = setupTime
	}
}

func (s *Stats) SetRunTime(runTime float64, gpu bool) {
	if gpu {
		s.RunTimeGPU = runTime
	} else {
		s.RunTimeCPU = runTime
	}
}

func (s *Stats) SetMemoryUsage(memoryUsage uint64, gpu bool) {
	if gpu {
		s.MemoryUsageGPU = memoryUsage
	} else {
		s.MemoryUsageCPU = memoryUsage
	}
}

func (s *Stats) SetCPUUsage(cpuUsage float64, gpu bool) {
	if gpu {
		s.CPUUsageGPU = cpuUsage
	} else {
		s.CPUUsageCPU = cpuUsage
	}
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
