// Package wheels provides configuration for sourcing cog and coglet wheels.
package wheels

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/replicate/cog/pkg/global"
)

var semverPreReleaseRe = regexp.MustCompile(`-alpha(\d+)|-beta(\d+)|-rc(\d+)|-dev(\d*)`)

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
//   - "https://..." or "http://..." - Direct wheel URL
//   - "/path/to/file.whl" or "relative/path" - Local file or directory (resolved to abspath)
//
// Paths that point to directories are resolved later by the Resolve functions,
// which glob for the appropriate wheel inside the directory.
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
		// Extract version after "pypi:" prefix, preserving original case
		return &WheelConfig{Source: WheelSourcePyPI, Version: value[5:]}
	}

	// Check for URL (http:// or https://)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return &WheelConfig{Source: WheelSourceURL, URL: value}
	}

	// Treat everything else as a file/directory path - resolve to absolute
	absPath, err := filepath.Abs(value)
	if err != nil {
		absPath = value
	}
	return &WheelConfig{Source: WheelSourceFile, Path: absPath}
}

// goarchToWheelPlatform maps GOARCH values to wheel filename platform substrings.
func goarchToWheelPlatform(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return ""
	}
}

// resolveWheelPath resolves a wheel path that may be a file or directory.
// If path is a directory, globs for pattern inside it, filtering by platform if non-empty.
// If path is a file, returns it directly.
func resolveWheelPath(path string, pattern string, platform string, envVar string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: path not found: %s", envVar, path)
	}

	if !info.IsDir() {
		return path, nil
	}

	matches, _ := filepath.Glob(filepath.Join(path, pattern))
	if len(matches) == 0 {
		return "", fmt.Errorf("%s: no wheel matching '%s' found in %s\n\nTo build the wheel, run: mise run build:sdk (for cog) or mise run build:coglet (for coglet)", envVar, pattern, path)
	}

	// Filter by platform if specified
	platStr := goarchToWheelPlatform(platform)
	if platStr != "" {
		var filtered []string
		for _, m := range matches {
			base := filepath.Base(m)
			if strings.Contains(base, platStr) || strings.Contains(base, "-none-any") {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			return "", fmt.Errorf("%s: no wheel for platform %s found in %s (found %d for other platforms)", envVar, platform, path, len(matches))
		}
		matches = filtered
	}

	if len(matches) > 1 {
		return "", fmt.Errorf("%s: multiple wheels matching '%s' in %s — specify the exact file path", envVar, pattern, path)
	}

	return matches[0], nil
}

// findWheelInCwdDist checks ./dist in the current working directory for a matching wheel.
// Returns the absolute path if found, empty string otherwise.
// Only checks cwd — does NOT chase REPO_ROOT. Used for auto-detection in dev builds
// where a missing wheel just means "fall back to PyPI".
func findWheelInCwdDist(pattern string) string {
	matches, _ := filepath.Glob(filepath.Join("dist", pattern))
	if len(matches) > 0 {
		absPath, _ := filepath.Abs(matches[0])
		if absPath != "" {
			return absPath
		}
		return matches[0]
	}
	return ""
}

// ResolveCogWheel resolves the WheelConfig for the cog SDK.
//
// Parameters:
//   - envValue: value of COG_WHEEL env var (empty string if not set)
//   - version: the CLI version (e.g. "dev", "0.17.0", "0.17.0-alpha1")
//
// Resolution order:
//  1. envValue (if non-empty, explicit override)
//  2. Auto-detect: check dist/cog-*.whl (for development builds only)
//  3. Default: PyPI (with version pin for release builds)
func ResolveCogWheel(envValue string, version string) (*WheelConfig, error) {
	// Check explicit env var first
	if config := ParseWheelValue(envValue); config != nil {
		if config.Source == WheelSourceFile {
			// cog SDK is pure Python (py3-none-any), no platform filtering needed
			resolved, err := resolveWheelPath(config.Path, "cog-*.whl", "", CogWheelEnvVar)
			if err != nil {
				return nil, err
			}
			config.Path = resolved
		}
		return config, nil
	}

	isDev := version == "dev" || strings.Contains(version, "-dev") || strings.Contains(version, "+")

	// Auto-detect for dev builds: check ./dist in cwd
	if isDev {
		if path := findWheelInCwdDist("cog-*.whl"); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}, nil
		}
	}

	// Default: PyPI
	config := &WheelConfig{Source: WheelSourcePyPI}
	if !isDev {
		config.Version = version
	}
	return config, nil
}

// GetCogWheelConfig is a convenience wrapper that reads COG_WHEEL from the environment
// and version from global.Version.
func GetCogWheelConfig() (*WheelConfig, error) {
	return ResolveCogWheel(os.Getenv(CogWheelEnvVar), global.Version)
}

// ResolveCogletWheel resolves the WheelConfig for coglet.
//
// Resolution order:
//  1. envValue (COGLET_WHEEL) if non-empty — explicit override
//  2. Auto-detect: check ./dist for coglet-*.whl (development builds only)
//  3. Default: PyPI (with version pinned for release builds)
//
// Coglet is always required. Returns a valid config or an error.
// The platform parameter is a GOARCH value (e.g. "amd64", "arm64") used to select
// the correct platform-specific wheel from a directory. Pass "" to skip filtering.
func ResolveCogletWheel(envValue string, version string, platform string) (*WheelConfig, error) {
	// Check explicit env var first
	if config := ParseWheelValue(envValue); config != nil {
		if config.Source == WheelSourceFile {
			resolved, err := resolveWheelPath(config.Path, "coglet-*.whl", platform, CogletWheelEnvVar)
			if err != nil {
				return nil, err
			}
			config.Path = resolved
		}
		return config, nil
	}

	isDev := version == "dev" || strings.Contains(version, "-dev") || strings.Contains(version, "+")

	// Auto-detect for dev builds: check ./dist in cwd
	if isDev {
		if path := findWheelInCwdDist("coglet-*.whl"); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}, nil
		}
	}

	// Default: PyPI
	config := &WheelConfig{Source: WheelSourcePyPI}
	if !isDev {
		config.Version = version
	}
	return config, nil
}

// GetCogletWheelConfig is a convenience wrapper that reads COGLET_WHEEL from the environment
// and version from global.Version. Does not filter by platform — use ResolveCogletWheel
// directly when platform selection is needed (e.g. Dockerfile generation).
func GetCogletWheelConfig() (*WheelConfig, error) {
	return ResolveCogletWheel(os.Getenv(CogletWheelEnvVar), global.Version, "")
}

// SemverToPEP440 converts a semver pre-release version to PEP 440 format.
// e.g. "0.17.0-alpha1" -> "0.17.0a1", "0.17.0-beta2" -> "0.17.0b2",
// "0.17.0-rc1" -> "0.17.0rc1", "0.17.0-dev1" -> "0.17.0.dev1"
// Stable versions pass through unchanged: "0.17.0" -> "0.17.0"
func SemverToPEP440(version string) string {
	return semverPreReleaseRe.ReplaceAllStringFunc(version, func(match string) string {
		match = strings.TrimPrefix(match, "-")
		match = strings.Replace(match, "alpha", "a", 1)
		match = strings.Replace(match, "beta", "b", 1)
		// rc stays as rc in PEP 440
		// dev -> .dev (PEP 440 uses dot separator)
		if strings.HasPrefix(match, "dev") {
			return "." + match
		}
		return match
	})
}

// PyPIPackageURL returns the pip install specifier for a PyPI package.
// If version is empty, returns just the package name (latest).
// Otherwise returns "package==version" with the version converted to PEP 440.
func (c *WheelConfig) PyPIPackageURL(packageName string) string {
	if c.Version == "" {
		return packageName
	}
	return packageName + "==" + SemverToPEP440(c.Version)
}
