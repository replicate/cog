package global

import (
	"time"
)

var (
	Version                              = "0.0.1"
	BuildTime                            = "none"
	Verbose                              = false
	ProfilingEnabled                     = false
	Profiler         interface{ Stop() } = nil
	StartupTimeout                       = 5 * time.Minute
	ConfigFilename                       = "cog.yaml"
	CogServerAddress                     = "http://cog.replicate.ai" // TODO(andreas): https
)
