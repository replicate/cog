package dockerfile

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util/console"
)

func envLineFromConfig(c *config.Config) (string, error) {
	vars, err := c.ParseEnvironment()
	if err != nil {
		return "", fmt.Errorf("failed to expand environment variables: %w", err)
	}
	if len(vars) == 0 {
		return "", nil
	}

	sortedNames := slices.Sorted(maps.Keys(vars))

	var r8PrefixNames, cogPrefixNames []string
	for _, name := range sortedNames {
		if strings.HasPrefix(name, "R8_") {
			r8PrefixNames = append(r8PrefixNames, name)
		} else if strings.HasPrefix(name, "COG_") {
			cogPrefixNames = append(cogPrefixNames, name)
		}
	}

	if len(r8PrefixNames) > 0 {
		console.Warnf("Environment variables starting with R8_ may have unintended behavior. (%v)", strings.Join(r8PrefixNames, " "))
	}
	if len(cogPrefixNames) > 0 {
		console.Warnf("Environment variables starting with COG_ may have unintended behavior. (%v)", strings.Join(cogPrefixNames, " "))
	}

	out := "ENV"
	for _, name := range sortedNames {
		out = out + " " + name + "=" + vars[name]
	}
	out += "\n"

	return out, nil
}
