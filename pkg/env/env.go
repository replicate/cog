package env

import "os"

const SchemeEnvVarName = "R8_SCHEME"
const PytorchHostEnvVarName = "R8_PYTORCH_HOST"

func SchemeFromEnvironment() string {
	scheme := os.Getenv(SchemeEnvVarName)
	if scheme == "" {
		scheme = "https"
	}
	return scheme
}

func PytorchHostFromEnvironment() string {
	host := os.Getenv(PytorchHostEnvVarName)
	if host == "" {
		host = "download.pytorch.org"
	}
	return host
}
