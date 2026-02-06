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

// findWheelInDist looks for a wheel file matching the pattern in the dist/ directory.
// Returns the absolute path to the wheel if found.
// Checks multiple locations: ./dist, <repo-root>/dist
func findWheelInDist(pattern string, envVar string) (string, error) {
	// First try ./dist in current directory
	matches, _ := filepath.Glob(filepath.Join("dist", pattern))
	if len(matches) > 0 {
		absPath, err := filepath.Abs(matches[0])
		if err != nil {
			return matches[0], nil
		}
		return absPath, nil
	}

	// Try repo root dist/
	repoRoot, err := getRepoRoot()
	if err != nil {
		return "", err
	}

	distDir := filepath.Join(repoRoot, "dist")
	matches, _ = filepath.Glob(filepath.Join(distDir, pattern))
	if len(matches) > 0 {
		return matches[0], nil
	}

	return "", fmt.Errorf("%s=dist: no wheel matching '%s' found in %s\n\nTo build the wheel, run: mise run build:sdk (for cog) or mise run build:coglet (for coglet)", envVar, pattern, distDir)
}

// findWheelInDistSilent is like findWheelInDist but returns empty string instead of error.
// Used for auto-detection where missing wheel is not an error.
func findWheelInDistSilent(pattern string) string {
	// First try ./dist in current directory
	matches, _ := filepath.Glob(filepath.Join("dist", pattern))
	if len(matches) > 0 {
		absPath, _ := filepath.Abs(matches[0])
		if absPath != "" {
			return absPath
		}
		return matches[0]
	}

	// Try repo root dist/
	repoRoot, err := getRepoRoot()
	if err != nil {
		return ""
	}

	matches, _ = filepath.Glob(filepath.Join(repoRoot, "dist", pattern))
	if len(matches) > 0 {
		return matches[0]
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
			path, err := findWheelInDist("cog-*.whl", CogWheelEnvVar)
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
		if path := findWheelInDistSilent("cog-*.whl"); path != "" {
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

// GetCogletWheelConfig returns the WheelConfig for coglet based on COGLET_WHEEL env var.
//
// Coglet is always opt-in via COGLET_WHEEL env var. Supported values:
//   - "dist" - Use local dist/ directory
//   - "pypi" or "pypi:version" - Install from PyPI
//   - URL or file path - Direct wheel location
//
// Returns nil, nil if coglet should not be installed (default).
// Returns nil, error if configuration is invalid.
func GetCogletWheelConfig() (*WheelConfig, error) {
	// Coglet is opt-in only via explicit env var
	config := ParseWheelValue(os.Getenv(CogletWheelEnvVar))
	if config == nil {
		return nil, nil
	}

	// Handle "dist" keyword - resolve to actual path
	if config.Source == WheelSourceFile && config.Path == "dist" {
		path, err := findWheelInDist("coglet-*.whl", CogletWheelEnvVar)
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

// PyPIPackageURL returns the pip install specifier for a PyPI package.
// If version is empty, returns just the package name (latest).
// Otherwise returns "package==version".
func (c *WheelConfig) PyPIPackageURL(packageName string) string {
	if c.Version == "" {
		return packageName
	}
	return packageName + "==" + c.Version
}
