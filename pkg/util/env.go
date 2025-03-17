package util

import (
	"os"

	"github.com/replicate/cog/pkg/util/console"
)

// GetEnvOrDefault returns an environment variable or a default if either the environment variable
// does not exist or fails to parse using the specified conversionFunc function
func GetEnvOrDefault[T any](key string, defaultVal T, conversionFunc func(string) (T, error)) T {
	val, exists := os.LookupEnv(key)
	if exists {
		v, err := conversionFunc(val)
		if err == nil {
			return v
		} else {
			console.Warnf("Failed to convert env var %s to expected type. Continuing with default. Error: %v", key, err)
		}
	}
	return defaultVal
}
