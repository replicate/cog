package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/config"
)

func TestPythonBlock_Name(t *testing.T) {
	block := &PythonBlock{}
	assert.Equal(t, "python", block.Name())
}

func TestPythonBlock_Detect(t *testing.T) {
	tests := []struct {
		name           string
		files          map[string]string
		pythonVersion  string
		expectedDetect bool
	}{
		{
			name: "python files present",
			files: map[string]string{
				"main.py": "print('hello')",
			},
			expectedDetect: true,
		},
		{
			name: "pyproject.toml present",
			files: map[string]string{
				"pyproject.toml": `[project]
name = "test"
version = "0.1.0"`,
			},
			expectedDetect: true,
		},
		{
			name: "requirements.txt present",
			files: map[string]string{
				"requirements.txt": "requests==2.31.0",
			},
			expectedDetect: true,
		},
		{
			name: "uv.lock present",
			files: map[string]string{
				"uv.lock": `version = 1
requires-python = ">=3.11"`,
			},
			expectedDetect: true,
		},
		{
			name: ".python-version present",
			files: map[string]string{
				".python-version": "3.11",
			},
			expectedDetect: true,
		},
		{
			name:           "python_version in config",
			files:          map[string]string{},
			pythonVersion:  "3.11",
			expectedDetect: true,
		},
		{
			name:           "no python indicators",
			files:          map[string]string{},
			expectedDetect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory with test files
			tmpDir := createTempDir(t, tt.files)
			defer os.RemoveAll(tmpDir)

			// Create source info
			cfg := &config.Config{
				Build: &config.Build{
					PythonVersion: tt.pythonVersion,
				},
			}
			src, err := project.NewSourceInfo(tmpDir, cfg)
			require.NoError(t, err)
			defer src.Close()

			block := &PythonBlock{}
			detected, err := block.Detect(t.Context(), src)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedDetect, detected)
		})
	}
}

func TestPythonBlock_Dependencies_VersionPriority(t *testing.T) {
	tests := []struct {
		name            string
		files           map[string]string
		pythonVersion   string
		expectedVersion string
	}{
		{
			name: "cog.yaml has priority",
			files: map[string]string{
				"uv.lock": `version = 1
requires-python = ">=3.10"`,
				".python-version": "3.9",
			},
			pythonVersion:   "3.11",
			expectedVersion: "3.11",
		},
		{
			name: "uv.lock second priority",
			files: map[string]string{
				"uv.lock": `version = 1
requires-python = ">=3.10"`,
				".python-version": "3.9",
			},
			expectedVersion: "3.10",
		},
		{
			name: ".python-version third priority",
			files: map[string]string{
				".python-version": "3.9",
				"pyproject.toml": `[project]
requires-python = ">=3.8"`,
			},
			expectedVersion: "3.9",
		},
		{
			name: "pyproject.toml fourth priority",
			files: map[string]string{
				"pyproject.toml": `[project]
requires-python = ">=3.8"`,
			},
			expectedVersion: "3.8",
		},
		{
			name:            "default to 3.12",
			files:           map[string]string{},
			expectedVersion: "3.12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory with test files
			tmpDir := createTempDir(t, tt.files)
			defer os.RemoveAll(tmpDir)

			// Create source info
			cfg := &config.Config{
				Build: &config.Build{
					PythonVersion: tt.pythonVersion,
				},
			}
			src, err := project.NewSourceInfo(tmpDir, cfg)
			require.NoError(t, err)
			defer src.Close()

			block := &PythonBlock{}
			deps, err := block.Dependencies(t.Context(), src)

			require.NoError(t, err)
			require.Len(t, deps, 1)

			dep := deps[0]
			assert.Equal(t, "python", dep.Name)
			assert.Equal(t, "python", dep.Provider)
			assert.Equal(t, tt.expectedVersion, dep.RequestedVersion)
		})
	}
}

func TestPythonBlock_Plan_NoInstallNeeded(t *testing.T) {
	// Create temporary directory (empty is fine for this test)
	tmpDir := createTempDir(t, map[string]string{})
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{Build: &config.Build{}}
	src, err := project.NewSourceInfo(tmpDir, cfg)
	require.NoError(t, err)
	defer src.Close()

	block := &PythonBlock{}

	// Create plan where Python is already present and correct version
	p := &plan.Plan{
		BaseImage: baseimg.BaseImage{
			Build:   "ubuntu:22.04",
			Runtime: "ubuntu:22.04",
			Metadata: baseimg.BaseImageMetadata{
				Packages: map[string]baseimg.Package{
					"python": {
						Name:    "python",
						Version: "3.12.1", // Compatible with 3.12
					},
				},
			},
		},
		Dependencies: map[string]plan.Dependency{
			"python": {
				Name:            "python",
				ResolvedVersion: "3.12",
			},
		},
	}

	err = block.Plan(t.Context(), src, p)
	require.NoError(t, err)

	// Should not add any stages
	totalStages := countStages(p)
	assert.Equal(t, 0, totalStages, "Should not add stages when Python is already present")
}

func TestPythonBlock_Plan_InstallNeeded(t *testing.T) {
	// Create temporary directory (empty is fine for this test)
	tmpDir := createTempDir(t, map[string]string{})
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{Build: &config.Build{}}
	src, err := project.NewSourceInfo(tmpDir, cfg)
	require.NoError(t, err)
	defer src.Close()

	block := &PythonBlock{}

	// Create plan where Python installation is needed
	p := &plan.Plan{
		BaseImage: baseimg.BaseImage{
			Build:   "ubuntu:22.04",
			Runtime: "ubuntu:22.04",
			Metadata: baseimg.BaseImageMetadata{
				Packages: map[string]baseimg.Package{
					// No Python in base image
				},
			},
		},
		Dependencies: map[string]plan.Dependency{
			"python": {
				Name:            "python",
				ResolvedVersion: "3.12",
			},
		},
	}

	err = block.Plan(t.Context(), src, p)
	require.NoError(t, err)

	// Should add installation stages
	totalStages := countStages(p)
	assert.Equal(t, 2, totalStages, "Should add build and export stages")

	// Verify stages have correct IDs
	installStage := p.GetStage("python-install")
	exportStage := p.GetStage("python-export")

	assert.NotNil(t, installStage, "Should have python-install stage")
	assert.NotNil(t, exportStage, "Should have python-export stage")

	// Verify install stage has UV command
	found := false
	for _, op := range installStage.Operations {
		if exec, ok := op.(plan.Exec); ok {
			if exec.Command == "uv python install 3.12" {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Should have UV python install command")
}

func TestPythonBlock_VersionCompatibility(t *testing.T) {
	block := &PythonBlock{}

	tests := []struct {
		name       string
		installed  string
		required   string
		compatible bool
	}{
		{
			name:       "exact match",
			installed:  "3.12",
			required:   "3.12",
			compatible: true,
		},
		{
			name:       "patch version compatible",
			installed:  "3.12.1",
			required:   "3.12",
			compatible: true,
		},
		{
			name:       "minor version incompatible",
			installed:  "3.11",
			required:   "3.12",
			compatible: false,
		},
		{
			name:       "major version incompatible",
			installed:  "2.7",
			required:   "3.12",
			compatible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := block.versionsCompatible(tt.installed, tt.required)
			assert.Equal(t, tt.compatible, result)
		})
	}
}

// Helper functions

func createTempDir(t *testing.T, files map[string]string) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "python-block-test-*")
	require.NoError(t, err)

	for filename, content := range files {
		filePath := filepath.Join(tmpDir, filename)

		// Create directory if needed
		dir := filepath.Dir(filePath)
		if dir != tmpDir {
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
		}

		err := os.WriteFile(filePath, []byte(content), 0644)
		require.NoError(t, err)
	}

	return tmpDir
}

func countStages(p *plan.Plan) int {
	count := 0
	for _, phase := range p.BuildPhases {
		count += len(phase.Stages)
	}
	for _, phase := range p.ExportPhases {
		count += len(phase.Stages)
	}
	return count
}
