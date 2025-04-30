package coglog

import (
	"os"
	"strconv"
)

const CoglogHostEnvVarName = "R8_COGLOG_HOST"
const CoglogDisableEnvVarName = "R8_COGLOG_DISABLE"

func HostFromEnvironment() string {
	host := os.Getenv(CoglogHostEnvVarName)
	if host == "" {
		host = "coglog.replicate.delivery"
	}
	return host
}

func DisableFromEnvironment() (bool, error) {
	disable := os.Getenv(CoglogDisableEnvVarName)
	if disable == "" {
		disable = "false"
	}
	return strconv.ParseBool(disable)
}
