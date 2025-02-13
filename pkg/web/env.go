package web

import "os"

const WebHostEnvVarName = "R8_WEB_HOST"

func HostFromEnvironment() string {
	host := os.Getenv(WebHostEnvVarName)
	if host == "" {
		host = "cog.replicate.com"
	}
	return host
}
