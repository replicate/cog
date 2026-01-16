package wheels

import (
	"embed"
	"fmt"
	"os"
	"strings"
)

//go:generate sh -c "rm -f cog-*.whl coglet-*.whl"
//go:generate sh -c "cp ../../dist/cog-*.whl ."
//go:generate sh -c "cp ../../dist/coglet-*.whl ."

//go:embed cog-*.whl coglet-*.whl
var wheelsFS embed.FS

func init() {
	assertWheelsEmbedded()
}

// assertWheelsEmbedded ensures wheels are embedded:
// - exactly 1 cog wheel (pure Python, platform-independent)
// - at least 1 coglet wheel (platform-specific, we embed multiple)
func assertWheelsEmbedded() {
	files, err := wheelsFS.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded wheels directory: %v", err))
	}

	var cogCount, cogletCount int
	for _, f := range files {
		name := f.Name()
		if strings.HasSuffix(name, ".whl") {
			if strings.HasPrefix(name, "coglet-") {
				cogletCount++
			} else if strings.HasPrefix(name, "cog-") {
				cogCount++
			}
		}
	}

	if cogCount != 1 {
		panic(fmt.Sprintf("expected exactly 1 cog wheel embedded, found %d - run 'make wheel' to fix", cogCount))
	}
	if cogletCount < 1 {
		panic(fmt.Sprintf("expected at least 1 coglet wheel embedded, found %d - run 'make wheel' to fix", cogletCount))
	}
}

func ReadCogWheel() (string, []byte) {
	return readWheelFromFS("cog-", "")
}

// ReadCogletWheel reads the coglet wheel for the specified platform.
// platform should be "linux_x86_64", "linux_aarch64", "macosx_arm64", etc.
// If platform is empty, returns the first coglet wheel found.
func ReadCogletWheel() (string, []byte) {
	return readWheelFromFS("coglet-", "")
}

// ReadCogletWheelForPlatform reads the coglet wheel matching the target platform.
// platform examples: "manylinux" (for any linux), "linux_x86_64", "linux_aarch64", "macosx"
func ReadCogletWheelForPlatform(platform string) (string, []byte) {
	return readWheelFromFS("coglet-", platform)
}

func readWheelFromFS(prefix string, platformHint string) (string, []byte) {
	files, err := wheelsFS.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded wheels: %v", err))
	}

	// First pass: look for exact platform match
	if platformHint != "" {
		for _, f := range files {
			name := f.Name()
			if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".whl") {
				if strings.Contains(name, platformHint) {
					data, err := wheelsFS.ReadFile(name)
					if err != nil {
						panic(fmt.Sprintf("failed to read embedded wheel %s: %v", name, err))
					}
					return name, data
				}
			}
		}
	}

	// Second pass: return first match
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) && strings.HasSuffix(f.Name(), ".whl") {
			data, err := wheelsFS.ReadFile(f.Name())
			if err != nil {
				panic(fmt.Sprintf("failed to read embedded wheel %s: %v", f.Name(), err))
			}
			return f.Name(), data
		}
	}
	panic(fmt.Sprintf("no %s*.whl wheel found in embedded filesystem - build is broken", prefix))
}

// WheelSource represents the source type for the wheel to install
type WheelSource int

const (
	// WheelSourceCog uses the embedded cog wheel (default)
	WheelSourceCog WheelSource = iota
	// WheelSourceCoglet uses the embedded Rust coglet wheel + cog wheel
	WheelSourceCoglet
	// WheelSourceURL uses a custom URL
	WheelSourceURL
	// WheelSourceFile uses a local file path
	WheelSourceFile
)

// String returns the string representation of the WheelSource
func (s WheelSource) String() string {
	switch s {
	case WheelSourceCog:
		return "cog"
	case WheelSourceCoglet:
		return "coglet"
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
	// Path is set when Source is WheelSourceFile
	Path string
}

// CogWheelEnvVar is the environment variable name for wheel selection
const CogWheelEnvVar = "COG_WHEEL"

// ParseCogWheel parses a COG_WHEEL value and returns the appropriate WheelConfig.
// Supported values:
//   - "cog" - Embedded cog wheel only (default)
//   - "coglet" - Embedded Rust coglet wheel + cog wheel, uses Rust HTTP server
//   - "https://..." or "http://..." - Direct wheel URL
//   - "/path/to/file.whl" or "./path/to/file.whl" - Local wheel file
//
// Returns nil if the value is empty (caller should use defaults).
func ParseCogWheel(value string) *WheelConfig {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	switch strings.ToLower(value) {
	case "cog":
		return &WheelConfig{Source: WheelSourceCog}
	case "coglet":
		return &WheelConfig{Source: WheelSourceCoglet}
	}

	// Check for URL (http:// or https://)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return &WheelConfig{Source: WheelSourceURL, URL: value}
	}

	// Treat everything else as a file path
	return &WheelConfig{Source: WheelSourceFile, Path: value}
}

// GetWheelConfig returns the WheelConfig based on COG_WHEEL env var.
// Priority:
//  1. COG_WHEEL env var (if set, overrides everything)
//  2. Default: embedded cog wheel
func GetWheelConfig() *WheelConfig {
	envValue := os.Getenv(CogWheelEnvVar)
	if config := ParseCogWheel(envValue); config != nil {
		return config
	}

	// Default to cog wheel
	return &WheelConfig{Source: WheelSourceCog}
}
