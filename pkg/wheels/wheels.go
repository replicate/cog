package wheels

import (
	_ "embed"
	"os"
	"strings"
)

//go:generate sh -c "cp ../../dist/cog-*.whl cog.whl"
//go:generate sh -c "cp ../../dist/coglet-*.whl coglet.whl"

//go:embed cog.whl
var cogWheel []byte

//go:embed coglet.whl
var cogletWheel []byte

func ReadCogWheel() (string, []byte) {
	return "cog.whl", cogWheel
}

func ReadCogletWheel() (string, []byte) {
	return "coglet.whl", cogletWheel
}

// WheelSource represents the source type for the wheel to install
type WheelSource int

const (
	// WheelSourceCog uses the embedded cog wheel (default when cog_runtime: false)
	WheelSourceCog WheelSource = iota
	// WheelSourceCogletEmbedded uses the embedded coglet wheel
	WheelSourceCogletEmbedded
	// WheelSourceCogletAlpha uses the PinnedCogletURL (default when cog_runtime: true)
	WheelSourceCogletAlpha
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
	case WheelSourceCogletEmbedded:
		return "coglet"
	case WheelSourceCogletAlpha:
		return "coglet-alpha"
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
//   - "cog" - Embedded cog wheel
//   - "coglet" - Embedded coglet wheel
//   - "coglet-alpha" - PinnedCogletURL
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
		return &WheelConfig{Source: WheelSourceCogletEmbedded}
	case "coglet-alpha":
		return &WheelConfig{Source: WheelSourceCogletAlpha}
	}

	// Check for URL (http:// or https://)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		return &WheelConfig{Source: WheelSourceURL, URL: value}
	}

	// Treat everything else as a file path
	return &WheelConfig{Source: WheelSourceFile, Path: value}
}

// GetWheelConfig returns the WheelConfig based on COG_WHEEL env var and cog_runtime flag.
// Priority:
//  1. COG_WHEEL env var (if set, overrides everything)
//  2. cog_runtime: true -> coglet-alpha (PinnedCogletURL)
//  3. cog_runtime: false (default) -> embedded cog wheel
func GetWheelConfig(cogRuntimeEnabled bool) *WheelConfig {
	envValue := os.Getenv(CogWheelEnvVar)
	if config := ParseCogWheel(envValue); config != nil {
		return config
	}

	// Default based on cog_runtime flag
	if cogRuntimeEnabled {
		return &WheelConfig{Source: WheelSourceCogletAlpha}
	}
	return &WheelConfig{Source: WheelSourceCog}
}
