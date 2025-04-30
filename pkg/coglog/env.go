package coglog

import "os"

const CoglogHostEnvVarName = "R8_COGLOG_HOST"

func HostFromEnvironment() string {
	host := os.Getenv(CoglogHostEnvVarName)
	if host == "" {
		host = "coglog.replicate.delivery"
	}
	return host
}
