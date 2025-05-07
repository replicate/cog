package dockerfile

import (
	"maps"
	"slices"

	"github.com/replicate/cog/pkg/config"
)

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
