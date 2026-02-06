package config

import (
	"fmt"
	"strings"
)

// EnvironmentVariableDenyList is a list of environment variable patterns that are
// used internally during build or runtime and thus not allowed to be set by the user.
// There are ways around this restriction, but it's likely to cause unexpected behavior
// and hard to debug issues. So on Cog's predict-build-push happy path, we don't allow
// these to be set.
// This list may change at any time. For more context, see:
// https://github.com/replicate/cog/pull/2274/#issuecomment-2831823185
var EnvironmentVariableDenyList = []string{
	// paths
	"PATH",
	"LD_LIBRARY_PATH",
	"PYTHONPATH",
	"VIRTUAL_ENV",
	"PYTHONUNBUFFERED",
	// Replicate
	"R8_*",
	"REPLICATE_*",
	// Nvidia
	"LIBRARY_PATH",
	"CUDA_*",
	"NVIDIA_*",
	"NV_*",
	// pget
	"PGET_*",
	"HF_ENDPOINT",
	"HF_HUB_ENABLE_HF_TRANSFER",
	// k8s
	"KUBERNETES_*",
}

// validateEnvName checks if the given environment variable name is allowed.
// Returns an error if the name matches any of the restricted patterns.
func validateEnvName(name string) error {
	for _, pattern := range EnvironmentVariableDenyList {
		// Check for exact match
		if pattern == name {
			return fmt.Errorf("environment variable %q is not allowed", name)
		}

		// Check for wildcard pattern
		if strings.HasSuffix(pattern, "*") {
			if strings.HasPrefix(name, pattern[:len(pattern)-1]) {
				return fmt.Errorf("environment variable %q is not allowed", name)
			}
		}
	}
	return nil
}

// parseAndValidateEnvironment converts a slice of strings in the format of KEY=VALUE
// to a map[string]string. An error is returned if the format is incorrect or if either
// the variable name or value are invalid.
func parseAndValidateEnvironment(input []string) (map[string]string, error) {
	env := map[string]string{}
	for _, input := range input {
		parts := strings.SplitN(input, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, fmt.Errorf("environment variable %q is not in the KEY=VALUE format", input)
		}
		if err := validateEnvName(parts[0]); err != nil {
			return nil, err
		}
		if _, ok := env[parts[0]]; ok {
			return nil, fmt.Errorf("environment variable %q is already defined", parts[0])
		}
		env[parts[0]] = parts[1]
	}
	return env, nil
}
