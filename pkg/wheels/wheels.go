package wheels

import (
	"embed"
	"fmt"
	"os"
	"strings"
)

//go:generate sh -c "rm -f cog-*.whl coglet-*.whl cog_dataclass-*.whl"
//go:generate sh -c "cp ../../dist/cog-*.whl ."
//go:generate sh -c "cp ../../dist/coglet-*.whl ."
//go:generate sh -c "if ls ../../cog-dataclass/dist/cog_dataclass-*.whl 1>/dev/null 2>&1; then cp ../../cog-dataclass/dist/cog_dataclass-*.whl .; else touch cog_dataclass-0.0.0-placeholder.whl; fi"

//go:embed cog-*.whl coglet-*.whl cog_dataclass-*.whl
var wheelsFS embed.FS

func init() {
	assertExactlyOneWheelPerRuntime()
}

// assertExactlyOneWheelPerRuntime ensures exactly 2 wheels are embedded (one cog, one coglet).
// If there are more or fewer, the build is broken - likely stale wheels left in pkg/wheels/
// or dist/, or the wheels weren't built at all. Panics on failure since this is a build-time
// invariant that must hold for the binary to function correctly.
func assertExactlyOneWheelPerRuntime() {
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
	if cogletCount != 1 {
		panic(fmt.Sprintf("expected exactly 1 coglet wheel embedded, found %d - run 'make wheel' to fix", cogletCount))
	}
}

func ReadCogWheel() (string, []byte) {
	return readWheelFromFS("cog-")
}

func ReadCogletWheel() (string, []byte) {
	return readWheelFromFS("coglet-")
}

// ReadCogDataclassWheel returns the embedded cog-dataclass wheel.
// Returns empty name and nil data if only a placeholder is embedded.
func ReadCogDataclassWheel() (string, []byte, bool) {
	files, err := wheelsFS.ReadDir(".")
	if err != nil {
		return "", nil, false
	}
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, "cog_dataclass-") && strings.HasSuffix(name, ".whl") {
			// Check if it's a placeholder (empty file)
			if strings.Contains(name, "placeholder") {
				return "", nil, false
			}
			data, err := wheelsFS.ReadFile(name)
			if err != nil || len(data) == 0 {
				return "", nil, false
			}
			return name, data, true
		}
	}
	return "", nil, false
}

func readWheelFromFS(prefix string) (string, []byte) {
	files, err := wheelsFS.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded wheels: %v", err))
	}
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
	// WheelSourceCog uses the embedded cog wheel (default when cog_runtime: false)
	WheelSourceCog WheelSource = iota
	// WheelSourceCogletEmbedded uses the embedded coglet wheel
	WheelSourceCogletEmbedded
	// WheelSourceCogletAlpha uses the PinnedCogletURL (default when cog_runtime: true)
	WheelSourceCogletAlpha
	// WheelSourceCogDataclass uses the embedded cog-dataclass wheel (pydantic-less)
	WheelSourceCogDataclass
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
	case WheelSourceCogDataclass:
		return "cog-dataclass"
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
//   - "cog-dataclass" - Embedded cog-dataclass wheel (pydantic-less)
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
	case "cog-dataclass":
		return &WheelConfig{Source: WheelSourceCogDataclass}
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
