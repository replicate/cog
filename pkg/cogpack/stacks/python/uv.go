package python

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/dockerfile"
)

// UvBlock handles uv-based Python dependency management
type UvBlock struct{}

// isUvProject checks if the source directory contains a UV project
func isUvProject(src *project.SourceInfo) bool {
	// Check for uv.lock or pyproject.toml
	if _, err := os.Stat(filepath.Join(src.RootPath(), "uv.lock")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(src.RootPath(), "pyproject.toml")); err == nil {
		return true
	}
	return false
}

// generatePyprojectToml creates a minimal pyproject.toml for non-UV projects
func generatePyprojectToml(packages []string, cogWheelPath string) string {
	var content strings.Builder
	
	content.WriteString(`[project]
name = "cog-model"
version = "0.1.0"
dependencies = [
`)
	
	// Add cog wheel as first dependency
	content.WriteString(fmt.Sprintf("    \"cog @ file://%s\",\n", cogWheelPath))
	
	// Add other packages
	for _, pkg := range packages {
		content.WriteString(fmt.Sprintf("    \"%s\",\n", pkg))
	}
	
	content.WriteString("]\n")
	
	return content.String()
}

func (b *UvBlock) Name() string { return "uv" }

func (b *UvBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	// always true for now
	return true, nil
}

func (b *UvBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]*plan.Dependency, error) {
	// UV itself doesn't require external dependencies
	return nil, nil
}

func (b *UvBlock) Plan(ctx context.Context, src *project.SourceInfo, composer *plan.Composer) error {
	pythonRuntime, ok := composer.GetDependency("python")
	if !ok {
		return fmt.Errorf("python dependency not found")
	}

	// Setup venv first
	venvStage, err := composer.AddStage(plan.PhaseAppDeps, "uv-venv", plan.WithName("Setup venv"))
	if err != nil {
		return err
	}
	venvStage.AddOperation(plan.Exec{
		Command: fmt.Sprintf("uv venv /venv --python %s", pythonRuntime.ResolvedVersion),
	})

	// Check if this is already a UV project
	if isUvProject(src) {
		// For UV projects, copy pyproject.toml and uv.lock, then sync
		syncStage, err := composer.AddStage(plan.PhaseAppDeps, "uv-sync", plan.WithName("Install dependencies"))
		if err != nil {
			return err
		}
		
		// Copy UV project files
		syncStage.AddOperation(plan.Copy{
			From: plan.Input{Local: "source"},
			Src:  []string{"pyproject.toml"},
			Dest: "/app/",
		})
		
		// Only copy uv.lock if it exists
		if _, err := os.Stat(filepath.Join(src.RootPath(), "uv.lock")); err == nil {
			syncStage.AddOperation(plan.Copy{
				From: plan.Input{Local: "source"},
				Src:  []string{"uv.lock"},
				Dest: "/app/",
			})
		}
		
		// Run uv sync (use working directory instead of cd)
		syncStage.Dir = "/app"
		syncStage.AddOperation(plan.Exec{
			Command: "uv sync --python /venv/bin/python --no-install-project",
		})
	} else {
		// For non-UV projects, generate pyproject.toml and sync
		packages, err := src.Config.PythonPackages()
		if err != nil {
			return fmt.Errorf("failed to get Python packages: %w", err)
		}
		
		// Get cog wheel filename
		wheelFilename, err := dockerfile.WheelFilename()
		if err != nil {
			return fmt.Errorf("failed to get wheel filename: %w", err)
		}
		
		// Generate pyproject.toml content
		pyprojectContent := generatePyprojectToml(packages, fmt.Sprintf("/mnt/wheel/embed/%s", wheelFilename))
		
		// Create in-memory filesystem with pyproject.toml
		pyprojectFS := fstest.MapFS{
			"pyproject.toml": &fstest.MapFile{Data: []byte(pyprojectContent)},
		}
		
		// Add pyproject.toml as build context
		composer.AddContext("uv-pyproject", &plan.BuildContext{
			Name:        "uv-pyproject",
			SourceBlock: "uv",
			Description: "Generated pyproject.toml for UV",
			FS:          pyprojectFS,
		})
		
		// Create sync stage
		syncStage, err := composer.AddStage(plan.PhaseAppDeps, "uv-sync", plan.WithName("Install dependencies"))
		if err != nil {
			return err
		}
		
		// Copy generated pyproject.toml
		syncStage.AddOperation(plan.Copy{
			From: plan.Input{Local: "uv-pyproject"},
			Src:  []string{"pyproject.toml"},
			Dest: "/app/",
		})
		
		// Run uv sync with wheel mount (use working directory instead of cd)
		syncStage.Dir = "/app"
		syncStage.AddOperation(plan.Exec{
			Command: "uv sync --python /venv/bin/python --no-install-project",
			Mounts: []plan.Mount{{
				Source: plan.Input{Local: "wheel-context"},
				Target: "/mnt/wheel",
			}},
		})
	}

	// Copy venv to export image
	exportStage, err := composer.AddStage(plan.ExportPhaseRuntime, "copy-venv")
	if err != nil {
		return err
	}
	
	// Remove existing venv to prevent nested directory creation
	exportStage.AddOperation(plan.Exec{
		Command: "rm -rf /venv",
	})
	
	// Copy venv from build stage
	exportStage.AddOperation(plan.Copy{
		From: plan.Input{Phase: plan.PhaseBuildComplete},
		Src:  []string{"/venv"},
		Dest: "/venv",
	})

	return nil
}

