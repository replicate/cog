package env

import "os"

const SchemeEnvVarName = "R8_SCHEME"
const MonobeamHostEnvVarName = "R8_MONOBEAM_HOST"
const WebHostEnvVarName = "R8_WEB_HOST"
const APIHostEnvVarName = "R8_API_HOST"
const PipelinesRuntimeHostEnvVarName = "R8_PIPELINES_RUNTIME_HOST"
const PytorchHostEnvVarName = "R8_PYTORCH_HOST"

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

func APIHostFromEnvironment() string {
	host := os.Getenv(APIHostEnvVarName)
	if host == "" {
		host = "api.replicate.com"
	}
	return host
}

func PipelinesRuntimeHostFromEnvironment() string {
	host := os.Getenv(PipelinesRuntimeHostEnvVarName)
	if host == "" {
		host = "pipelines-runtime.replicate.delivery"
	}
	return host
}

func PytorchHostFromEnvironment() string {
	host := os.Getenv(PytorchHostEnvVarName)
	if host == "" {
		host = "download.pytorch.org"
	}
	return host
}
