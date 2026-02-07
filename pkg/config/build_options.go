package config

// BuildOptions contains runtime options passed via CLI flags, not from cog.yaml.
// These are separate from the Config struct because they are not part of the
// model configuration - they are build-time settings that affect how the
// container is built but not what's in it.
type BuildOptions struct {
	// SourceEpochTimestamp is the number of seconds since Unix epoch to use
	// for the build timestamp. Set to -1 to disable timestamp rewrites.
	// This is useful for reproducible builds.
	SourceEpochTimestamp int64

	// XCachePath is the path to the BuildKit cache directory.
	// If empty, inline caching is used instead of local cache.
	XCachePath string
}

// DefaultBuildOptions returns BuildOptions with sensible defaults.
func DefaultBuildOptions() BuildOptions {
	return BuildOptions{
		SourceEpochTimestamp: -1,
		XCachePath:           "",
	}
}
