// Package wheels provides configuration for sourcing cog and coglet wheels.
package wheels

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/replicate/cog/pkg/global"
	cogversion "github.com/replicate/cog/pkg/util/version"
)

var semverPreReleaseRe = regexp.MustCompile(`-alpha(\d+)|-beta(\d+)|-rc(\d+)|-dev(\d*)`)

// pep440PreReleaseRe matches PEP 440 pre-release identifiers (a1, b2, rc1, .dev1)
var pep440PreReleaseRe = regexp.MustCompile(`\d(a|b|rc|\.dev)\d`)

// IsPreRelease returns true if the version string contains a pre-release identifier
// in either semver (-alpha1, -beta2, -rc1, -dev1) or PEP 440 (a1, b2, rc1, .dev1) format.
func IsPreRelease(version string) bool {
	return semverPreReleaseRe.MatchString(version) || pep440PreReleaseRe.MatchString(version)
}

// MinimumSDKVersion is the minimum cog SDK version that can be explicitly requested.
// Versions older than this lack features required by the current CLI.
const MinimumSDKVersion = "0.16.0"

// BaseVersionRe extracts the MAJOR.MINOR.PATCH prefix, ignoring pre-release suffixes.
var BaseVersionRe = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

// ValidateSDKVersion checks that a PyPI WheelConfig does not request a version
// older than MinimumSDKVersion. Non-PyPI sources, unpinned versions, and nil
// configs are always valid.
func ValidateSDKVersion(config *WheelConfig, label string) error {
	if config == nil || config.Source != WheelSourcePyPI || config.Version == "" {
		return nil
	}
	base := config.Version
	if m := BaseVersionRe.FindString(base); m != "" {
		base = m
	}
	reqVer, err := cogversion.NewVersion(base)
	if err != nil {
		return nil // unparseable — let pip catch real problems
	}
	minVer := cogversion.MustVersion(MinimumSDKVersion)
	if reqVer.GreaterOrEqual(minVer) {
		return nil
	}
	return fmt.Errorf("%s version %s is below the minimum required version %s", label, config.Version, MinimumSDKVersion)
}

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

// CogSDKWheelEnvVar is the environment variable name for cog SDK wheel selection
const CogSDKWheelEnvVar = "COG_SDK_WHEEL"

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

var executablePath = os.Executable
var evalSymlinks = filepath.EvalSymlinks

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

func bestWheelMatch(matches []string, platform string) string {
	if len(matches) == 0 {
		return ""
	}
	if platform != "" {
		platStr := goarchToWheelPlatform(platform)
		if platStr != "" {
			var filtered []string
			for _, match := range matches {
				base := filepath.Base(match)
				if strings.Contains(base, platStr) || strings.Contains(base, "-none-any") {
					filtered = append(filtered, match)
				}
			}
			matches = filtered
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// distFromExecutable returns the dist/ directory relative to the running cog
// binary, if it appears to be in a goreleaser output layout (dist/go/<platform>/cog).
// Returns empty string if the path cannot be determined.
func distFromExecutable() string {
	exePath, err := executablePath()
	if err != nil {
		return ""
	}
	exePath, err = evalSymlinks(exePath)
	if err != nil {
		return ""
	}

	distDir := filepath.Clean(filepath.Join(filepath.Dir(exePath), "..", ".."))
	if info, err := os.Stat(distDir); err == nil && info.IsDir() {
		return distDir
	}
	return ""
}

// findWheelInAutoDetectDist checks ./dist and dist relative to the cog executable.
// Returns the absolute path if found, empty string otherwise.
func findWheelInAutoDetectDist(pattern string, platform string) string {
	matches, _ := filepath.Glob(filepath.Join("dist", pattern))
	if best := bestWheelMatch(matches, platform); best != "" {
		absPath, _ := filepath.Abs(best)
		if absPath != "" {
			return absPath
		}
		return best
	}

	if distDir := distFromExecutable(); distDir != "" {
		matches, _ = filepath.Glob(filepath.Join(distDir, pattern))
		if best := bestWheelMatch(matches, platform); best != "" {
			return best
		}
	}

	return ""
}

// DetectLocalSDKVersion checks dist/ (CWD and executable-relative) for a cog
// SDK wheel and extracts the version from its filename. Returns empty string if
// no local wheel is found.
func DetectLocalSDKVersion() string {
	path := findWheelInAutoDetectDist("cog-*.whl", "")
	if path == "" {
		return ""
	}
	// Wheel filename format: cog-<version>-<python>-<abi>-<platform>.whl
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "cog-") {
		return ""
	}
	rest := strings.TrimPrefix(base, "cog-")
	if idx := strings.Index(rest, "-"); idx > 0 {
		return rest[:idx]
	}
	return ""
}

// resolveWheelPath resolves a wheel path that may be a file or directory.
// If path is a directory, globs for pattern inside it, filtering by platform if non-empty.
// If path is a file, returns it directly.
func resolveWheelPath(path string, pattern string, platform string, envVar string) (string, error) {
	info, err := os.Stat(path) //nolint:gosec // G703: path from build config, not user input
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

// ResolveCogWheel resolves the WheelConfig for the cog SDK.
//
// Parameters:
//   - envValue: value of COG_SDK_WHEEL env var (empty string if not set)
//   - version: the CLI version (e.g. "dev", "0.17.0", "0.17.0-alpha1")
//
// Resolution order:
//  1. envValue (if non-empty, explicit override)
//  2. Auto-detect: check dist/cog-*.whl (for development builds only)
//  3. Default: PyPI latest (use build.sdk_version in cog.yaml to pin)
func ResolveCogWheel(envValue string, version string) (*WheelConfig, error) {
	// Check explicit env var first
	if config := ParseWheelValue(envValue); config != nil {
		if config.Source == WheelSourceFile {
			// cog SDK is pure Python (py3-none-any), no platform filtering needed
			resolved, err := resolveWheelPath(config.Path, "cog-*.whl", "", CogSDKWheelEnvVar)
			if err != nil {
				return nil, err
			}
			config.Path = resolved
		}
		return config, nil
	}

	isDev := version == "dev" || strings.Contains(version, "-dev") || strings.Contains(version, "+")

	// Auto-detect for dev builds: check ./dist or executable-relative dist
	if isDev {
		if path := findWheelInAutoDetectDist("cog-*.whl", ""); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}, nil
		}
	}

	// Default: PyPI (always latest; use sdk_version in cog.yaml to pin)
	return &WheelConfig{Source: WheelSourcePyPI}, nil
}

// GetCogWheelConfig is a convenience wrapper that reads COG_SDK_WHEEL from the environment
// and version from global.Version.
func GetCogWheelConfig() (*WheelConfig, error) {
	return ResolveCogWheel(os.Getenv(CogSDKWheelEnvVar), global.Version)
}

// ResolveCogletWheel resolves the WheelConfig for coglet.
//
// targetArch is the GOARCH of the Docker build target (e.g. "amd64", "arm64").
// It is used to select the correct platform-specific wheel from dist/.
//
// Resolution order:
//  1. envValue (COGLET_WHEEL) if non-empty — explicit override
//  2. Auto-detect: check ./dist for coglet-*.whl (development builds only)
//  3. Default: PyPI latest (use COGLET_WHEEL=pypi:x.y.z to pin)
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

	// Auto-detect for dev builds: check ./dist or executable-relative dist
	if isDev {
		if path := findWheelInAutoDetectDist("coglet-*.whl", platform); path != "" {
			return &WheelConfig{Source: WheelSourceFile, Path: path}, nil
		}
	}

	// Default: PyPI (always latest; use COGLET_WHEEL=pypi:x.y.z to pin)
	return &WheelConfig{Source: WheelSourcePyPI}, nil
}

// GetCogletWheelConfig is a convenience wrapper that reads COGLET_WHEEL from the environment
// and version from global.Version. targetArch is the GOARCH of the Docker build target
// (e.g. "amd64", "arm64") used to select the correct platform-specific wheel.
func GetCogletWheelConfig(targetArch string) (*WheelConfig, error) {
	return ResolveCogletWheel(os.Getenv(CogletWheelEnvVar), global.Version, targetArch)
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
