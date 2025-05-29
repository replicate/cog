package env

import "os"

const SchemeEnvVarName = "R8_SCHEME"
const MonobeamHostEnvVarName = "R8_MONOBEAM_HOST"
const WebHostEnvVarName = "R8_WEB_HOST"

func SchemeFromEnvironment() string {
	scheme := os.Getenv(SchemeEnvVarName)
	if scheme == "" {
		scheme = "https"
	}
	return scheme
}

func MonobeamHostFromEnvironment() string {
	host := os.Getenv(MonobeamHostEnvVarName)
	if host == "" {
		host = "monobeam.replicate.delivery"
	}
	return host
}

func WebHostFromEnvironment() string {
	host := os.Getenv(WebHostEnvVarName)
	if host == "" {
		host = "cog.replicate.com"
	}
	return host
}
