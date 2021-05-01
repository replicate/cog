package global

import (
	"time"
)

var (
	Version          = "0.0.1"
	BuildTime        = "none"
	Verbose          = false
	ProfilingEnabled = false
	StartupTimeout   = 5 * time.Minute
	ConfigFilename   = "cog.yaml"
	CogServerAddress = "https://cog.replicate.ai"
)
