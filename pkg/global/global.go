package global

import (
	"time"
)

var (
	Version               = "dev"
	Commit                = ""
	BuildTime             = "none"
	Verbose               = false
	ProfilingEnabled      = false
	StartupTimeout        = 5 * time.Minute
	ConfigFilename        = "cog.yaml"
	ReplicateRegistryHost = "r8.im"
	LabelNamespace        = "org.cogmodel."
)
