package wheels

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadCogWheel(t *testing.T) {
	filename, data := ReadCogWheel()
	require.True(t, strings.HasPrefix(filename, "cog-"), "filename should start with 'cog-', got: %s", filename)
	require.True(t, strings.HasSuffix(filename, ".whl"), "filename should end with '.whl', got: %s", filename)
	require.Greater(t, len(data), 10000)
}

func TestReadCogletWheel(t *testing.T) {
	filename, data := ReadCogletWheel()
	require.True(t, strings.HasPrefix(filename, "coglet-"), "filename should start with 'coglet-', got: %s", filename)
	require.True(t, strings.HasSuffix(filename, ".whl"), "filename should end with '.whl', got: %s", filename)
	require.Greater(t, len(data), 1000000)
}

func TestWheelSourceString(t *testing.T) {
	tests := []struct {
		source   WheelSource
		expected string
	}{
		{WheelSourceCog, "cog"},
		{WheelSourceCogletEmbedded, "coglet"},
		{WheelSourceCogletAlpha, "coglet-alpha"},
		{WheelSourceURL, "url"},
		{WheelSourceFile, "file"},
		{WheelSource(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.source.String())
		})
	}
}

func TestParseCogWheel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *WheelConfig
	}{
		// Empty/nil cases
		{
			name:     "empty string returns nil",
			input:    "",
			expected: nil,
		},
		{
			name:     "whitespace only returns nil",
			input:    "   ",
			expected: nil,
		},

		// Named values
		{
			name:     "cog keyword",
			input:    "cog",
			expected: &WheelConfig{Source: WheelSourceCog},
		},
		{
			name:     "cog uppercase",
			input:    "COG",
			expected: &WheelConfig{Source: WheelSourceCog},
		},
		{
			name:     "cog mixed case",
			input:    "Cog",
			expected: &WheelConfig{Source: WheelSourceCog},
		},
		{
			name:     "coglet keyword",
			input:    "coglet",
			expected: &WheelConfig{Source: WheelSourceCogletEmbedded},
		},
		{
			name:     "coglet uppercase",
			input:    "COGLET",
			expected: &WheelConfig{Source: WheelSourceCogletEmbedded},
		},
		{
			name:     "coglet-alpha keyword",
			input:    "coglet-alpha",
			expected: &WheelConfig{Source: WheelSourceCogletAlpha},
		},
		{
			name:     "coglet-alpha uppercase",
			input:    "COGLET-ALPHA",
			expected: &WheelConfig{Source: WheelSourceCogletAlpha},
		},
		{
			name:     "cog with whitespace",
			input:    "  cog  ",
			expected: &WheelConfig{Source: WheelSourceCog},
		},

		// URLs
		{
			name:  "https URL",
			input: "https://example.com/wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceURL,
				URL:    "https://example.com/wheel.whl",
			},
		},
		{
			name:  "http URL",
			input: "http://example.com/wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceURL,
				URL:    "http://example.com/wheel.whl",
			},
		},
		{
			name:  "github release URL",
			input: "https://github.com/replicate/cog-runtime/releases/download/v0.1.0/coglet-0.1.0-py3-none-any.whl",
			expected: &WheelConfig{
				Source: WheelSourceURL,
				URL:    "https://github.com/replicate/cog-runtime/releases/download/v0.1.0/coglet-0.1.0-py3-none-any.whl",
			},
		},

		// File paths
		{
			name:  "absolute path",
			input: "/path/to/wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceFile,
				Path:   "/path/to/wheel.whl",
			},
		},
		{
			name:  "relative path with ./",
			input: "./dist/wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceFile,
				Path:   "./dist/wheel.whl",
			},
		},
		{
			name:  "relative path without ./",
			input: "dist/wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceFile,
				Path:   "dist/wheel.whl",
			},
		},
		{
			name:  "windows-style path",
			input: "C:\\path\\to\\wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceFile,
				Path:   "C:\\path\\to\\wheel.whl",
			},
		},
		{
			name:  "path with spaces",
			input: "/path/to/my wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceFile,
				Path:   "/path/to/my wheel.whl",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCogWheel(tt.input)
			if tt.expected == nil {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.expected.Source, result.Source)
				require.Equal(t, tt.expected.URL, result.URL)
				require.Equal(t, tt.expected.Path, result.Path)
			}
		})
	}
}

func TestGetWheelConfig(t *testing.T) {
	tests := []struct {
		name              string
		envValue          string
		cogRuntimeEnabled bool
		expected          *WheelConfig
	}{
		// Default behavior without env var
		{
			name:              "default with cog_runtime false",
			envValue:          "",
			cogRuntimeEnabled: false,
			expected:          &WheelConfig{Source: WheelSourceCog},
		},
		{
			name:              "default with cog_runtime true",
			envValue:          "",
			cogRuntimeEnabled: true,
			expected:          &WheelConfig{Source: WheelSourceCogletAlpha},
		},

		// Env var overrides cog_runtime
		{
			name:              "env cog overrides cog_runtime true",
			envValue:          "cog",
			cogRuntimeEnabled: true,
			expected:          &WheelConfig{Source: WheelSourceCog},
		},
		{
			name:              "env coglet overrides cog_runtime false",
			envValue:          "coglet",
			cogRuntimeEnabled: false,
			expected:          &WheelConfig{Source: WheelSourceCogletEmbedded},
		},
		{
			name:              "env coglet-alpha overrides cog_runtime false",
			envValue:          "coglet-alpha",
			cogRuntimeEnabled: false,
			expected:          &WheelConfig{Source: WheelSourceCogletAlpha},
		},
		{
			name:              "env URL overrides cog_runtime",
			envValue:          "https://example.com/custom.whl",
			cogRuntimeEnabled: false,
			expected: &WheelConfig{
				Source: WheelSourceURL,
				URL:    "https://example.com/custom.whl",
			},
		},
		{
			name:              "env file path overrides cog_runtime",
			envValue:          "/custom/path/wheel.whl",
			cogRuntimeEnabled: true,
			expected: &WheelConfig{
				Source: WheelSourceFile,
				Path:   "/custom/path/wheel.whl",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env var for test
			if tt.envValue != "" {
				t.Setenv(CogWheelEnvVar, tt.envValue)
			}

			result := GetWheelConfig(tt.cogRuntimeEnabled)
			require.NotNil(t, result)
			require.Equal(t, tt.expected.Source, result.Source)
			require.Equal(t, tt.expected.URL, result.URL)
			require.Equal(t, tt.expected.Path, result.Path)
		})
	}
}
