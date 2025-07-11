package python

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// PythonBlock handles Python version detection and installation
type PythonBlock struct{}

// Name returns the human-readable name of this block
func (b *PythonBlock) Name() string {
	return "python"
}

// Detect analyzes the project to determine if this is a Python project
func (b *PythonBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	// Check for Python indicators
	pythonIndicators := []string{
		"*.py",
		"pyproject.toml",
		"requirements.txt",
		"setup.py",
		"uv.lock",
		".python-version",
	}

	for _, pattern := range pythonIndicators {
		if src.FS.GlobExists(pattern) {
			return true, nil
		}
	}

	// Also check if Python version is explicitly specified in config
	if src.Config.Build != nil && src.Config.Build.PythonVersion != "" {
		return true, nil
	}

	return false, nil
}

// Dependencies emits Python version dependency from various sources
func (b *PythonBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]*plan.Dependency, error) {
	pythonVersion, err := b.detectPythonVersion(src)
	if err != nil {
		return nil, fmt.Errorf("failed to detect Python version: %w", err)
	}

	return []*plan.Dependency{
		{
			Name:             "python",
			Provider:         "python",
			RequestedVersion: pythonVersion,
			Source:           "multiple", // Could be cog.yaml, uv.lock, etc.
		},
	}, nil
}

// Plan creates installation stages if Python is not available or wrong version
func (b *PythonBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// Check if installation is needed
	if !b.needsInstallation(p) {
		return nil
	}

	// Get the resolved Python version
	pythonDep, exists := p.Dependencies["python"]
	if !exists {
		return fmt.Errorf("python dependency not found in resolved dependencies")
	}

	pythonVersion := pythonDep.ResolvedVersion

	// Create build stage for Python installation
	buildStage, err := p.AddStage(plan.PhaseRuntime, "Install Python", "python-install")
	if err != nil {
		return fmt.Errorf("failed to add python install stage: %w", err)
	}

	// Set source to build image
	buildStage.Source = plan.Input{Image: p.BaseImage.Build}

	// Set UV environment variables
	buildStage.Env = []string{
		"UV_COMPILE_BYTECODE=1",
		"UV_LINK_MODE=copy",
		"UV_PYTHON_INSTALL_DIR=/python",
		"UV_PYTHON_PREFERENCE=only-managed",
	}

	// Add the UV install operation
	buildStage.Operations = []plan.Op{
		plan.Exec{
			Command: fmt.Sprintf("uv python install %s", pythonVersion),
			Mounts: []plan.Mount{
				{
					Source: "image:ghcr.io/astral-sh/uv:latest",
					Target: "/uv",
				},
			},
		},
	}

	buildStage.Provides = []string{"python"}

	// Create export stage for Python
	exportStage, err := p.AddStage(plan.ExportPhaseRuntime, "Export Python", "python-export")
	if err != nil {
		return fmt.Errorf("failed to add python export stage: %w", err)
	}

	// Set source to runtime image
	exportStage.Source = plan.Input{Image: p.BaseImage.Runtime}

	// Copy Python installation from build stage
	exportStage.Operations = []plan.Op{
		plan.Copy{
			From: "python-install",
			Src:  []string{"/python"},
			Dest: "/python",
		},
	}

	// Set environment variables for Python
	exportStage.Env = []string{
		"PATH=/python/bin:$PATH",
		"PYTHONPATH=/python/lib/python" + pythonVersion + "/site-packages",
	}

	exportStage.Provides = []string{"runtime-python"}

	return nil
}

// detectPythonVersion determines Python version from various sources in priority order
func (b *PythonBlock) detectPythonVersion(src *project.SourceInfo) (string, error) {
	// 1. cog.yaml (highest priority)
	if src.Config.Build != nil && src.Config.Build.PythonVersion != "" {
		return src.Config.Build.PythonVersion, nil
	}

	// 2. uv.lock
	if src.FS.GlobExists("uv.lock") {
		version, err := b.parseUvLock(src)
		if err == nil && version != "" {
			return version, nil
		}
	}

	// 3. .python-version
	if src.FS.GlobExists(".python-version") {
		version, err := b.parsePythonVersionFile(src)
		if err == nil && version != "" {
			return version, nil
		}
	}

	// 4. pyproject.toml
	if src.FS.GlobExists("pyproject.toml") {
		version, err := b.parsePyprojectToml(src)
		if err == nil && version != "" {
			return version, nil
		}
	}

	// 5. Default to 3.12
	return "3.12", nil
}

// needsInstallation checks if Python installation is needed
func (b *PythonBlock) needsInstallation(p *plan.Plan) bool {
	// Get required Python version
	pythonDep, exists := p.Dependencies["python"]
	if !exists {
		return false
	}

	requiredVersion := pythonDep.ResolvedVersion

	// Check if base image has Python
	if pythonPkg, exists := p.BaseImage.Metadata.Packages["python"]; exists {
		// Check if versions are compatible
		return !b.versionsCompatible(pythonPkg.Version, requiredVersion)
	}

	// Python not in base image, installation needed
	return true
}

// versionsCompatible checks if installed version is compatible with required version
func (b *PythonBlock) versionsCompatible(installed, required string) bool {
	// For now, simple major.minor matching
	// TODO: Implement proper semver comparison
	installedMajorMinor := b.extractMajorMinor(installed)
	requiredMajorMinor := b.extractMajorMinor(required)

	return installedMajorMinor == requiredMajorMinor
}

// extractMajorMinor extracts major.minor from version string
func (b *PythonBlock) extractMajorMinor(version string) string {
	// Handle versions like "3.12.1" -> "3.12"
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// File parsing helpers

func (b *PythonBlock) parseUvLock(src *project.SourceInfo) (string, error) {
	// Read uv.lock file
	data, err := fs.ReadFile(src.FS, "uv.lock")
	if err != nil {
		return "", err
	}

	// Parse for requires-python line
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "requires-python = ") {
			// Extract version from 'requires-python = ">=3.10"'
			version := strings.Trim(line[len("requires-python = "):], `"`)
			// Remove version constraint operators (>=, ==, etc.)
			version = strings.TrimPrefix(version, ">=")
			version = strings.TrimPrefix(version, "==")
			version = strings.TrimPrefix(version, "~=")
			version = strings.TrimPrefix(version, "^")
			return strings.TrimSpace(version), nil
		}
	}

	return "", nil
}

func (b *PythonBlock) parsePythonVersionFile(src *project.SourceInfo) (string, error) {
	// Read .python-version file
	data, err := fs.ReadFile(src.FS, ".python-version")
	if err != nil {
		return "", err
	}

	// .python-version files contain just the version string
	version := strings.TrimSpace(string(data))
	return version, nil
}

func (b *PythonBlock) parsePyprojectToml(src *project.SourceInfo) (string, error) {
	// Read pyproject.toml file
	data, err := fs.ReadFile(src.FS, "pyproject.toml")
	if err != nil {
		return "", err
	}

	// Simple parsing for requires-python in [project] section
	lines := strings.Split(string(data), "\n")
	inProjectSection := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "[project]" {
			inProjectSection = true
			continue
		}

		if strings.HasPrefix(line, "[") && line != "[project]" {
			inProjectSection = false
			continue
		}

		if inProjectSection && strings.HasPrefix(line, "requires-python = ") {
			// Extract version from 'requires-python = ">=3.8"'
			version := strings.Trim(line[len("requires-python = "):], `"`)
			// Remove version constraint operators
			version = strings.TrimPrefix(version, ">=")
			version = strings.TrimPrefix(version, "==")
			version = strings.TrimPrefix(version, "~=")
			version = strings.TrimPrefix(version, "^")
			return strings.TrimSpace(version), nil
		}
	}

	return "", nil
}
