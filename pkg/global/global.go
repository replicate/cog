package global

import "os"

const (
	DefaultReplicateRegistryHost = "r8.im"
)

var (
	Version                 = "dev"
	Commit                  = ""
	BuildTime               = "none"
	Debug                   = false
	ProfilingEnabled        = false
	ReplicateRegistryHost   = getDefaultRegistryHost()
	ReplicateWebsiteHost    = "replicate.com"
	LabelNamespace          = "run.cog."
	CogBuildArtifactsFolder = ".cog"
)

func getDefaultRegistryHost() string {
	// Priority: flag will override at runtime, but env var provides default
	if host := os.Getenv("COG_REGISTRY_HOST"); host != "" {
		return host
	}
	return DefaultReplicateRegistryHost
}
