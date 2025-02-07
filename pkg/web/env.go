package web

import "os"

const WebHostEnvVarName = "R8_WEB_HOST"

func HostFromEnvironment() string {
	scheme := os.Getenv(WebHostEnvVarName)
	if scheme == "" {
		scheme = "replicate.com"
	}
	return scheme
}
