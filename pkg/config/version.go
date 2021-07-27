package config

import "time"

type Version struct {
	ID       string            `json:"id"`
	Config   *Config           `json:"config"`
	Created  time.Time         `json:"created"`
	BuildIDs map[string]string `json:"build_ids"`
}

type Image struct {
	URI          string                  `json:"uri"`
	Created      time.Time               `json:"created"`
	RunArguments map[string]*RunArgument `json:"run_arguments"`
	TestStats    *Stats                  `json:"test_stats"`
	BuildFailed  bool                    `json:"build_failed"`
}

type Stats struct {
	BootTime    float64 `json:"boot_time"`
	SetupTime   float64 `json:"setup_time"`
	RunTime     float64 `json:"run_time"`
	MemoryUsage uint64  `json:"memory_usage"`
	CPUUsage    float64 `json:"cpu_usage"`
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
