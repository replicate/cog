package monobeam

import "os"

const MonobeamHostEnvVarName = "R8_MONOBEAM_HOST"

func HostFromEnvironment() string {
	host := os.Getenv(MonobeamHostEnvVarName)
	if host == "" {
		host = "monobeam.replicate.delivery"
	}
	return host
}
