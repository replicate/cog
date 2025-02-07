package env

import "os"

const SchemeEnvVarName = "R8_SCHEME"

func SchemeFromEnvironment() string {
	scheme := os.Getenv(SchemeEnvVarName)
	if scheme == "" {
		scheme = "https"
	}
	return scheme
}
