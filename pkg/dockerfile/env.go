package dockerfile

import (
	"maps"
	"os"
	"slices"

	"github.com/replicate/cog/pkg/config"
)

const CogletVersionEnvVarName = "R8_COGLET_VERSION"

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
