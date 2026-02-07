package wheels

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/global"
)

func TestWheelSourceString(t *testing.T) {
	tests := []struct {
		source   WheelSource
		expected string
	}{
		{WheelSourcePyPI, "pypi"},
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

func TestParseWheelValue(t *testing.T) {
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

		// PyPI values
		{
			name:     "pypi keyword",
			input:    "pypi",
			expected: &WheelConfig{Source: WheelSourcePyPI},
		},
		{
			name:     "pypi uppercase",
			input:    "PYPI",
			expected: &WheelConfig{Source: WheelSourcePyPI},
		},
		{
			name:     "pypi with version",
			input:    "pypi:0.12.0",
			expected: &WheelConfig{Source: WheelSourcePyPI, Version: "0.12.0"},
		},
		{
			name:     "pypi with version uppercase",
			input:    "PYPI:1.0.0",
			expected: &WheelConfig{Source: WheelSourcePyPI, Version: "1.0.0"},
		},

		// dist keyword
		{
			name:     "dist keyword",
			input:    "dist",
			expected: &WheelConfig{Source: WheelSourceFile, Path: "dist"},
		},
		{
			name:     "dist uppercase",
			input:    "DIST",
			expected: &WheelConfig{Source: WheelSourceFile, Path: "dist"},
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
			input: "https://github.com/replicate/cog/releases/download/v0.1.0/cog-0.1.0-py3-none-any.whl",
			expected: &WheelConfig{
				Source: WheelSourceURL,
				URL:    "https://github.com/replicate/cog/releases/download/v0.1.0/cog-0.1.0-py3-none-any.whl",
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
				// Path will be converted to absolute
			},
		},
		{
			name:  "relative path without ./",
			input: "path/to/wheel.whl",
			expected: &WheelConfig{
				Source: WheelSourceFile,
				// Path will be converted to absolute
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseWheelValue(tt.input)
			if tt.expected == nil {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.expected.Source, result.Source)
				require.Equal(t, tt.expected.URL, result.URL)
				// For relative paths, just verify they're converted to absolute
				if tt.expected.Path == "" && result.Source == WheelSourceFile {
					require.True(t, filepath.IsAbs(result.Path), "path should be absolute: %s", result.Path)
				} else {
					require.Equal(t, tt.expected.Path, result.Path)
				}
				require.Equal(t, tt.expected.Version, result.Version)
			}
		})
	}
}

func TestGetCogWheelConfig(t *testing.T) {
	// Save and restore global.Version
	origVersion := global.Version
	defer func() { global.Version = origVersion }()

	// Create temp dir for file path tests and to avoid auto-detect from repo root
	tmpDir := t.TempDir()
	wheelFile := filepath.Join(tmpDir, "custom.whl")
	require.NoError(t, os.WriteFile(wheelFile, []byte("fake wheel"), 0o600))

	// Change to temp dir and clear REPO_ROOT to prevent auto-detection from repo dist/
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { require.NoError(t, os.Chdir(origDir)) }()
	t.Setenv("REPO_ROOT", "")

	tests := []struct {
		name           string
		envValue       string
		globalVersion  string
		expectedSource WheelSource
		expectedPath   string
		expectedURL    string
		expectedVer    string
	}{
		// Release build defaults to PyPI with version
		{
			name:           "release build defaults to PyPI with version",
			envValue:       "",
			globalVersion:  "0.12.0",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "0.12.0",
		},
		// Dev build defaults to PyPI (no local wheel in this temp dir)
		{
			name:           "dev build defaults to PyPI without version",
			envValue:       "",
			globalVersion:  "dev",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "",
		},
		// Snapshot build (goreleaser) defaults to PyPI without version
		{
			name:           "snapshot build defaults to PyPI without version",
			envValue:       "",
			globalVersion:  "0.16.12-dev+g6793b492",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "",
		},
		// Explicit pypi override
		{
			name:           "explicit pypi",
			envValue:       "pypi",
			globalVersion:  "dev",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "",
		},
		{
			name:           "explicit pypi with version",
			envValue:       "pypi:0.11.0",
			globalVersion:  "0.12.0",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "0.11.0",
		},
		// URL override
		{
			name:           "URL override",
			envValue:       "https://example.com/custom.whl",
			globalVersion:  "0.12.0",
			expectedSource: WheelSourceURL,
			expectedURL:    "https://example.com/custom.whl",
		},
		// File path override (use the real temp file)
		{
			name:           "file path override",
			envValue:       wheelFile,
			globalVersion:  "0.12.0",
			expectedSource: WheelSourceFile,
			expectedPath:   wheelFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			global.Version = tt.globalVersion
			if tt.envValue != "" {
				t.Setenv(CogWheelEnvVar, tt.envValue)
			}

			result, err := GetCogWheelConfig()
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.expectedSource, result.Source)
			require.Equal(t, tt.expectedURL, result.URL)
			require.Equal(t, tt.expectedPath, result.Path)
			require.Equal(t, tt.expectedVer, result.Version)
		})
	}
}

func TestGetCogWheelConfigErrors(t *testing.T) {
	// Test error cases for wheel config
	t.Run("file not found", func(t *testing.T) {
		t.Setenv(CogWheelEnvVar, "/nonexistent/path/wheel.whl")
		_, err := GetCogWheelConfig()
		require.Error(t, err)
		require.Contains(t, err.Error(), "wheel file not found")
	})
}

func TestGetCogWheelConfigAutoDetect(t *testing.T) {
	// Save and restore global.Version
	origVersion := global.Version
	defer func() { global.Version = origVersion }()

	// Create a temp directory with a wheel file
	tmpDir := t.TempDir()
	distDir := filepath.Join(tmpDir, "dist")
	require.NoError(t, os.MkdirAll(distDir, 0o750))

	wheelPath := filepath.Join(distDir, "cog-0.1.0-py3-none-any.whl")
	require.NoError(t, os.WriteFile(wheelPath, []byte("fake wheel content"), 0o600))

	// Change to temp dir
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { require.NoError(t, os.Chdir(origDir)) }()

	// Test auto-detection in dev mode
	global.Version = "dev"
	result, err := GetCogWheelConfig()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, WheelSourceFile, result.Source)
	require.Contains(t, result.Path, "cog-0.1.0-py3-none-any.whl")

	// Test that release mode does NOT auto-detect
	global.Version = "0.12.0"
	result, err = GetCogWheelConfig()
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, WheelSourcePyPI, result.Source)
	require.Equal(t, "0.12.0", result.Version)
}

func TestGetCogletWheelConfig(t *testing.T) {
	// Save and restore global.Version
	origVersion := global.Version
	defer func() { global.Version = origVersion }()

	// Change to temp dir and clear REPO_ROOT to prevent auto-detection from repo dist/
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { require.NoError(t, os.Chdir(origDir)) }()
	t.Setenv("REPO_ROOT", "")

	tests := []struct {
		name           string
		envValue       string
		globalVersion  string
		expectedSource WheelSource
		expectedPath   string
		expectedURL    string
		expectedVer    string
	}{
		// Default: coglet from PyPI (release build)
		{
			name:           "release default uses PyPI with version",
			envValue:       "",
			globalVersion:  "0.12.0",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "0.12.0",
		},
		{
			name:           "dev default falls back to PyPI without version",
			envValue:       "",
			globalVersion:  "dev",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "",
		},
		// Explicit pypi
		{
			name:           "explicit pypi",
			envValue:       "pypi",
			globalVersion:  "0.12.0",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "",
		},
		{
			name:           "explicit pypi with version",
			envValue:       "pypi:0.11.0",
			globalVersion:  "0.12.0",
			expectedSource: WheelSourcePyPI,
			expectedVer:    "0.11.0",
		},
		// URL override
		{
			name:           "URL override",
			envValue:       "https://example.com/coglet.whl",
			globalVersion:  "0.12.0",
			expectedSource: WheelSourceURL,
			expectedURL:    "https://example.com/coglet.whl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			global.Version = tt.globalVersion
			if tt.envValue != "" {
				t.Setenv(CogletWheelEnvVar, tt.envValue)
			}

			result, err := GetCogletWheelConfig()
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.expectedSource, result.Source)
			require.Equal(t, tt.expectedURL, result.URL)
			require.Equal(t, tt.expectedPath, result.Path)
			require.Equal(t, tt.expectedVer, result.Version)
		})
	}
}

func TestPyPIPackageURL(t *testing.T) {
	tests := []struct {
		name        string
		config      *WheelConfig
		packageName string
		expected    string
	}{
		{
			name:        "no version",
			config:      &WheelConfig{Source: WheelSourcePyPI},
			packageName: "cog",
			expected:    "cog",
		},
		{
			name:        "with version",
			config:      &WheelConfig{Source: WheelSourcePyPI, Version: "0.12.0"},
			packageName: "cog",
			expected:    "cog==0.12.0",
		},
		{
			name:        "coglet with version",
			config:      &WheelConfig{Source: WheelSourcePyPI, Version: "0.1.0"},
			packageName: "coglet",
			expected:    "coglet==0.1.0",
		},
		{
			name:        "alpha pre-release converted to PEP 440",
			config:      &WheelConfig{Source: WheelSourcePyPI, Version: "0.17.0-alpha1"},
			packageName: "cog",
			expected:    "cog==0.17.0a1",
		},
		{
			name:        "beta pre-release converted to PEP 440",
			config:      &WheelConfig{Source: WheelSourcePyPI, Version: "0.17.0-beta2"},
			packageName: "cog",
			expected:    "cog==0.17.0b2",
		},
		{
			name:        "rc pre-release converted to PEP 440",
			config:      &WheelConfig{Source: WheelSourcePyPI, Version: "1.0.0-rc1"},
			packageName: "cog",
			expected:    "cog==1.0.0rc1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.PyPIPackageURL(tt.packageName)
			require.Equal(t, tt.expected, result)
		})
	}
}
