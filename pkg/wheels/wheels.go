package wheels

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/replicate/cog/pkg/global"
)

// WheelSource represents the source type for the wheel to install
type WheelSource int

const (
	// WheelSourcePyPI installs from PyPI (default for released builds)
	WheelSourcePyPI WheelSource = iota
	// WheelSourceURL uses a custom URL
	WheelSourceURL
	// WheelSourceFile uses a local file path
	WheelSourceFile
)

// String returns the string representation of the WheelSource
func (s WheelSource) String() string {
	switch s {
	case WheelSourcePyPI:
		return "pypi"
	case WheelSourceURL:
		return "url"
	case WheelSourceFile:
		return "file"
	default:
		return "unknown"
	}
}

// WheelConfig represents the configuration for which wheel to install
type WheelConfig struct {
	// Source indicates where the wheel comes from
	Source WheelSource
	// URL is set when Source is WheelSourceURL
	URL string
	// Path is set when Source is WheelSourceFile (absolute path)
	Path string
	// Version is set when Source is WheelSourcePyPI (optional, empty = latest)
	Version string
}

// CogWheelEnvVar is the environment variable name for cog SDK wheel selection
const CogWheelEnvVar = "COG_WHEEL"

// CogletWheelEnvVar is the environment variable name for coglet wheel selection
const CogletWheelEnvVar = "COGLET_WHEEL"

// ParseWheelValue parses a wheel env var value and returns the appropriate WheelConfig.
// Supported values:
//   - "pypi" - Install from PyPI (latest version)
//   - "pypi:0.12.0" - Install specific version from PyPI
//   - "dist" - Use local dist/ directory (error if not found)
//   - "https://..." or "http://..." - Direct wheel URL
//   - "/path/to/file.whl" or "./path/to/file.whl" - Local wheel file
//
// Returns nil if the value is empty (caller should use auto-detection).
func ParseWheelValue(value string) *WheelConfig {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	// "pypi" or "pypi:version" requests PyPI
	if strings.EqualFold(value, "pypi") {
		return &WheelConfig{Source: WheelSourcePyPI}
	}
	if strings.HasPrefix(strings.ToLower(value), "pypi:") {
		version := strings.TrimPrefix(value, "pypi:")
		version = strings.TrimPrefix(version, "PyPI:")
		version = strings.TrimPrefix(value[5:], "") // preserve original case after colon
		return &WheelConfig{Source: WheelSourcePyPI, Version: value[5:]}
	}

	// Check for URL (http:// or https://)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return &WheelConfig{Source: WheelSourceURL, URL: value}
	}

	// "dist" keyword means look in dist/ directory
	if strings.EqualFold(value, "dist") {
		// This signals to use dist/ - actual path resolution happens in GetCogWheelConfig
		return &WheelConfig{Source: WheelSourceFile, Path: "dist"}
	}

	// Treat everything else as a file path
	return &WheelConfig{Source: WheelSourceFile, Path: value}
}

// findWheelInDist looks for a wheel file matching the pattern in the dist/ directory.
// Returns the absolute path to the wheel if found, empty string otherwise.
func findWheelInDist(pattern string) string {
	distDir := "dist"
	matches, err := filepath.Glob(filepath.Join(distDir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Return the first match (there should typically be only one)
	absPath, err := filepath.Abs(matches[0])
	if err != nil {
		return matches[0]
	}
	return absPath
}

// isDevVersion returns true if the version is a development/snapshot build.
// This includes "dev", versions containing "-dev", and versions with "+" (local versions).
func isDevVersion() bool {
	v := global.Version
	return v == "dev" || strings.Contains(v, "-dev") || strings.Contains(v, "+")
}

// GetCogWheelConfig returns the WheelConfig for the cog SDK based on COG_WHEEL env var.
//
// Resolution order:
//  1. COG_WHEEL env var (if set, explicit override)
//  2. Auto-detect: check dist/cog-*.whl (for development)
//  3. Default: PyPI
//
// For development builds (snapshot versions), auto-detection is enabled.
// For release builds, auto-detection is skipped (always PyPI unless overridden).
func GetCogWheelConfig() *WheelConfig {
	// Check explicit env var first
	if config := ParseWheelValue(os.Getenv(CogWheelEnvVar)); config != nil {
		// Handle "dist" keyword - resolve to actual path
		if config.Source == WheelSourceFile && config.Path == "dist" {
			path := findWheelInDist("cog-*.whl")
			if path == "" {
				// Explicit dist requested but not found - this is an error condition
				// Return the config anyway, caller will handle the missing file error
				config.Path = "dist/cog-*.whl"
			} else {
				config.Path = path
			}
		}
		return config
	}

	// Auto-detect for dev builds: check dist/ directory
	if isDevVersion() {
		if path := findWheelInDist("cog-*.whl"); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}
		}
	}

	// Default: PyPI
	// For release builds, use the matching version
	// For dev builds where no local wheel found, use latest
	config := &WheelConfig{Source: WheelSourcePyPI}
	if !isDevVersion() {
		config.Version = global.Version
	}
	return config
}

// GetCogletWheelConfig returns the WheelConfig for coglet based on COGLET_WHEEL env var.
//
// Resolution order:
//  1. COGLET_WHEEL env var (if set, explicit override)
//  2. Auto-detect: check dist/coglet-*.whl (for development)
//  3. Default: nil (coglet is optional, not installed by default)
//
// Returns nil if coglet should not be installed.
func GetCogletWheelConfig() *WheelConfig {
	// Check explicit env var first
	if config := ParseWheelValue(os.Getenv(CogletWheelEnvVar)); config != nil {
		// Handle "dist" keyword - resolve to actual path
		if config.Source == WheelSourceFile && config.Path == "dist" {
			path := findWheelInDist("coglet-*.whl")
			if path == "" {
				// Explicit dist requested but not found - this is an error condition
				config.Path = "dist/coglet-*.whl"
			} else {
				config.Path = path
			}
		}
		return config
	}

	// Auto-detect for dev builds: check dist/ directory
	if isDevVersion() {
		if path := findWheelInDist("coglet-*.whl"); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}
		}
	}

	// Default: no coglet (it's optional)
	return nil
}

// PyPIPackageURL returns the pip install specifier for a PyPI package.
// If version is empty, returns just the package name (latest).
// Otherwise returns "package==version".
func (c *WheelConfig) PyPIPackageURL(packageName string) string {
	if c.Version == "" {
		return packageName
	}
	return packageName + "==" + c.Version
}
