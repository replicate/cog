package dockerfile

import (
	"maps"
	"os"
	"slices"

	"github.com/replicate/cog/pkg/config"
)

const CogletVersionEnvVarName = "R8_COGLET_VERSION"
const MonobaseMatrixHostVarName = "R8_MONOBASE_MATRIX_HOST"
const MonobaseMatrixSchemeVarName = "R8_MONOBASE_MATRIX_SCHEME"

func envLineFromConfig(c *config.Config) (string, error) {
	vars := c.ParsedEnvironment()
	if len(vars) == 0 {
		return "", nil
	}

	out := "ENV"
	for _, name := range slices.Sorted(maps.Keys(vars)) {
		out = out + " " + name + "=" + vars[name]
	}
	out += "\n"

	return out, nil
}

func CogletVersionFromEnvironment() string {
	host := os.Getenv(CogletVersionEnvVarName)
	if host == "" {
		host = "coglet"
	}
	return host
}

func MonobaseMatrixHostFromEnvironment() string {
	host := os.Getenv(MonobaseMatrixHostVarName)
	if host == "" {
		host = "raw.githubusercontent.com"
	}
	return host
}

func MonobaseMatrixSchemeFromEnvironment() string {
	scheme := os.Getenv(MonobaseMatrixSchemeVarName)
	if scheme == "" {
		scheme = "https"
	}
	return scheme
}
