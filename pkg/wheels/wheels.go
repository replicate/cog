package wheels

import (
	"embed"
	"fmt"
	"os"
	"strings"
)

//go:generate sh -c "rm -f cog-*.whl"
//go:generate sh -c "cp ../../dist/cog-*.whl ."

//go:embed cog-*.whl
var wheelsFS embed.FS

func init() {
	assertExactlyOneWheel()
}

// assertExactlyOneWheel ensures exactly 1 cog wheel is embedded.
// If there are more or fewer, the build is broken - likely stale wheels left in pkg/wheels/
// or dist/, or the wheel wasn't built at all. Panics on failure since this is a build-time
// invariant that must hold for the binary to function correctly.
func assertExactlyOneWheel() {
	files, err := wheelsFS.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded wheels directory: %v", err))
	}

	var cogCount int
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, "cog-") && strings.HasSuffix(name, ".whl") {
			cogCount++
		}
	}

	if cogCount != 1 {
		panic(fmt.Sprintf("expected exactly 1 cog wheel embedded, found %d - run 'make wheel' to fix", cogCount))
	}
}

// ReadCogWheel returns the embedded cog wheel filename and contents.
func ReadCogWheel() (string, []byte) {
	files, err := wheelsFS.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded wheels: %v", err))
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "cog-") && strings.HasSuffix(f.Name(), ".whl") {
			data, err := wheelsFS.ReadFile(f.Name())
			if err != nil {
				panic(fmt.Sprintf("failed to read embedded wheel %s: %v", f.Name(), err))
			}
			return f.Name(), data
		}
	}
	panic("no cog-*.whl wheel found in embedded filesystem - build is broken")
}

// WheelSource represents the source type for the wheel to install
type WheelSource int

const (
	// WheelSourceEmbedded uses the embedded cog wheel (default)
	WheelSourceEmbedded WheelSource = iota
	// WheelSourceURL uses a custom URL
	WheelSourceURL
	// WheelSourceFile uses a local file path
	WheelSourceFile
)

// String returns the string representation of the WheelSource
func (s WheelSource) String() string {
	switch s {
	case WheelSourceEmbedded:
		return "embedded"
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
//   - "cog" - Embedded cog wheel (default)
//   - "https://..." or "http://..." - Direct wheel URL
//   - "/path/to/file.whl" or "./path/to/file.whl" - Local wheel file
//
// Returns nil if the value is empty (caller should use defaults).
func ParseCogWheel(value string) *WheelConfig {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	// "cog" explicitly requests embedded wheel
	if strings.EqualFold(value, "cog") {
		return &WheelConfig{Source: WheelSourceEmbedded}
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
//  1. COG_WHEEL env var (if set, overrides default)
//  2. Default: embedded cog wheel
func GetWheelConfig() *WheelConfig {
	if config := ParseCogWheel(os.Getenv(CogWheelEnvVar)); config != nil {
		return config
	}
	return &WheelConfig{Source: WheelSourceEmbedded}
}
