package monobeam

import "os"

const MonobeamHostEnvVarName = "R8_MONOBEAM_HOST"

func HostFromEnvironment() string {
	scheme := os.Getenv(MonobeamHostEnvVarName)
	if scheme == "" {
		scheme = "monobeam.replicate.delivery"
	}
	return scheme
}
