// Package wheels provides configuration for sourcing cog and coglet wheels.
package wheels

import (
	"fmt"
	"os"
	"os/exec"
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
		// Extract version after "pypi:" prefix, preserving original case
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

	// Treat everything else as a file path - resolve to absolute path
	absPath, err := filepath.Abs(value)
	if err != nil {
		// If we can't resolve, use the original path
		absPath = value
	}
	return &WheelConfig{Source: WheelSourceFile, Path: absPath}
}

// getRepoRoot returns the root of the repository.
// It checks (in order):
//  1. REPO_ROOT environment variable (set by mise)
//  2. git rev-parse --show-toplevel
//
// Returns an error if neither method succeeds.
func getRepoRoot() (string, error) {
	// Check REPO_ROOT env var first (set by mise)
	if repoRoot := os.Getenv("REPO_ROOT"); repoRoot != "" {
		return repoRoot, nil
	}

	// Fall back to git
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		// Check if git command exists
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			return "", fmt.Errorf("cannot locate repository root: git is not installed and REPO_ROOT is not set\n\nSet REPO_ROOT environment variable or run from within a git repository")
		}
		// git command exists but we're not in a repo
		return "", fmt.Errorf("cannot locate repository root: not inside a git repository and REPO_ROOT is not set\n\nSet REPO_ROOT environment variable or run from within a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// distFromExecutable returns the dist/ directory relative to the running cog
// binary, if it appears to be in a goreleaser output layout (dist/go/<platform>/cog).
// Returns empty string if the path cannot be determined.
func distFromExecutable() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return ""
	}
	// Binary is at dist/go/<platform>/cog → go up 2 levels to dist/
	distDir := filepath.Clean(filepath.Join(filepath.Dir(exePath), "..", ".."))
	if info, err := os.Stat(distDir); err == nil && info.IsDir() {
		return distDir
	}
	return ""
}

// findWheelInDist looks for a wheel file matching the pattern in the dist/ directory.
// Returns the absolute path to the wheel if found.
// Checks multiple locations: ./dist, <repo-root>/dist, <executable-relative>/dist
// If platformTag is non-empty, only wheels whose filename contains the tag are considered.
// When multiple wheels match, the last one in lexicographic order is used (highest version).
func findWheelInDist(pattern string, envVar string, platformTag string) (string, error) {
	// First try ./dist in current directory
	matches, _ := filepath.Glob(filepath.Join("dist", pattern))
	matches = filterWheelsByPlatform(matches, platformTag)
	if len(matches) > 0 {
		best := matches[len(matches)-1]
		absPath, err := filepath.Abs(best)
		if err != nil {
			return best, nil
		}
		return absPath, nil
	}

	// Try repo root dist/
	repoRoot, err := getRepoRoot()
	if err == nil {
		distDir := filepath.Join(repoRoot, "dist")
		matches, _ = filepath.Glob(filepath.Join(distDir, pattern))
		matches = filterWheelsByPlatform(matches, platformTag)
		if len(matches) > 0 {
			return matches[len(matches)-1], nil
		}
	}

	// Try dist/ relative to the cog executable
	if distDir := distFromExecutable(); distDir != "" {
		matches, _ = filepath.Glob(filepath.Join(distDir, pattern))
		matches = filterWheelsByPlatform(matches, platformTag)
		if len(matches) > 0 {
			return matches[len(matches)-1], nil
		}
	}

	return "", fmt.Errorf("%s=dist: no wheel matching '%s' found\n\nTo build the wheel, run: mise run build:sdk (for cog) or mise run build:coglet (for coglet)", envVar, pattern)
}

// findWheelInDistSilent is like findWheelInDist but returns empty string instead of error.
// Used for auto-detection where missing wheel is not an error.
// If platformTag is non-empty, only wheels whose filename contains the tag are considered.
// When multiple wheels match, the last one in lexicographic order is used (highest version).
func findWheelInDistSilent(pattern string, platformTag string) string {
	// First try ./dist in current directory
	matches, _ := filepath.Glob(filepath.Join("dist", pattern))
	matches = filterWheelsByPlatform(matches, platformTag)
	if len(matches) > 0 {
		best := matches[len(matches)-1]
		absPath, _ := filepath.Abs(best)
		if absPath != "" {
			return absPath
		}
		return best
	}

	// Try repo root dist/
	repoRoot, err := getRepoRoot()
	if err == nil {
		matches, _ = filepath.Glob(filepath.Join(repoRoot, "dist", pattern))
		matches = filterWheelsByPlatform(matches, platformTag)
		if len(matches) > 0 {
			return matches[len(matches)-1]
		}
	}

	// Try dist/ relative to the cog executable
	if distDir := distFromExecutable(); distDir != "" {
		matches, _ = filepath.Glob(filepath.Join(distDir, pattern))
		matches = filterWheelsByPlatform(matches, platformTag)
		if len(matches) > 0 {
			return matches[len(matches)-1]
		}
	}

	return ""
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
func GetCogWheelConfig() (*WheelConfig, error) {
	// Check explicit env var first
	if config := ParseWheelValue(os.Getenv(CogWheelEnvVar)); config != nil {
		// Handle "dist" keyword - resolve to actual path
		if config.Source == WheelSourceFile && config.Path == "dist" {
			path, err := findWheelInDist("cog-*.whl", CogWheelEnvVar, "")
			if err != nil {
				return nil, err
			}
			config.Path = path
		}
		// Verify file path exists
		if config.Source == WheelSourceFile && config.Path != "" {
			if _, err := os.Stat(config.Path); os.IsNotExist(err) {
				return nil, fmt.Errorf("%s: wheel file not found: %s", CogWheelEnvVar, config.Path)
			}
		}
		return config, nil
	}

	// Auto-detect for dev builds: check dist/ directory
	if isDevVersion() {
		if path := findWheelInDistSilent("cog-*.whl", ""); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}, nil
		}
	}

	// Default: PyPI
	// For release builds, use the matching version
	// For dev builds where no local wheel found, use latest
	config := &WheelConfig{Source: WheelSourcePyPI}
	if !isDevVersion() {
		config.Version = global.Version
	}
	return config, nil
}

// wheelPlatformTag returns the wheel platform substring to match for a given
// GOARCH when targeting Linux containers. For example, "amd64" → "x86_64",
// "arm64" → "aarch64". Returns empty string if targetArch is empty (no filtering).
func wheelPlatformTag(targetArch string) string {
	switch targetArch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return ""
	}
}

// filterWheelsByPlatform filters a list of wheel paths to only those matching
// the target platform tag. If platformTag is empty, returns all matches unchanged.
func filterWheelsByPlatform(matches []string, platformTag string) []string {
	if platformTag == "" {
		return matches
	}
	var filtered []string
	for _, m := range matches {
		if strings.Contains(filepath.Base(m), platformTag) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// GetCogletWheelConfig returns the WheelConfig for coglet based on COGLET_WHEEL env var.
//
// targetArch is the GOARCH of the Docker build target (e.g. "amd64", "arm64").
// It is used to select the correct platform-specific wheel from dist/.
//
// Resolution order:
//  1. COGLET_WHEEL env var (if set, explicit override)
//  2. Auto-detect: check dist/coglet-*.whl (for development)
//  3. Default: PyPI
//
// Returns nil only on error. Coglet is always installed.
func GetCogletWheelConfig(targetArch string) (*WheelConfig, error) {
	platformTag := wheelPlatformTag(targetArch)

	// Check explicit env var first
	if config := ParseWheelValue(os.Getenv(CogletWheelEnvVar)); config != nil {
		// Handle "dist" keyword - resolve to actual path
		if config.Source == WheelSourceFile && config.Path == "dist" {
			path, err := findWheelInDist("coglet-*.whl", CogletWheelEnvVar, platformTag)
			if err != nil {
				return nil, err
			}
			config.Path = path
		}
		// Verify file path exists
		if config.Source == WheelSourceFile && config.Path != "" {
			if _, err := os.Stat(config.Path); os.IsNotExist(err) {
				return nil, fmt.Errorf("%s: wheel file not found: %s", CogletWheelEnvVar, config.Path)
			}
		}
		return config, nil
	}

	// Auto-detect for dev builds: check dist/ directory
	if isDevVersion() {
		if path := findWheelInDistSilent("coglet-*.whl", platformTag); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}, nil
		}
	}

	// Default: PyPI
	config := &WheelConfig{Source: WheelSourcePyPI}
	if !isDevVersion() {
		config.Version = global.Version
	}
	return config, nil
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
