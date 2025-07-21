package python

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/config"
)

func TestUvBlock_BasicDetection(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Build: &config.Build{
			PythonVersion: "3.11",
		},
	}
	src, err := project.NewSourceInfo(tmpDir, cfg)
	require.NoError(t, err)

	block := &UvBlock{}

	// Test basic detection
	detected, err := block.Detect(ctx, src)
	require.NoError(t, err)
	assert.True(t, detected, "UV block should always be detected")

	// Test name
	assert.Equal(t, "uv", block.Name())

	// Test dependencies
	deps, err := block.Dependencies(ctx, src)
	require.NoError(t, err)
	assert.Nil(t, deps, "UV block should not report external dependencies")
}

func TestUvBlock_VenvCreation(t *testing.T) {
	ctx := context.Background()
	composer := plan.NewPlanComposer()

	// Set up a python dependency
	composer.SetDependencies(map[string]*plan.Dependency{
		"python": {
			Name:            "python",
			ResolvedVersion: "3.11",
		},
	})

	// Add base stages so UV has something to build upon
	setupBaseStages(t, composer)

	// Create source info with no packages (just venv creation)
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Build: &config.Build{
			PythonVersion: "3.11",
		},
	}
	err := cfg.ValidateAndComplete(tmpDir)
	require.NoError(t, err)

	src, err := project.NewSourceInfo(tmpDir, cfg)
	require.NoError(t, err)

	// Create and run the UV block
	block := &UvBlock{}
	err = block.Plan(ctx, src, composer)
	require.NoError(t, err)

	// Compose the plan
	composedPlan, err := composer.Compose()
	require.NoError(t, err)

	// Check that we have the expected UV-related stages
	venvStage := findStageByID(composedPlan.Stages, "uv-venv")
	syncStage := findStageByID(composedPlan.Stages, "uv-sync")
	copyStage := findStageByID(composedPlan.Stages, "copy-venv")

	// When no packages are specified, we should have venv setup, sync (for cog wheel), and copy
	require.NotNil(t, venvStage, "uv-venv stage should exist")
	require.NotNil(t, syncStage, "uv-sync stage should exist even with no user packages (for cog wheel)")
	require.NotNil(t, copyStage, "copy-venv stage should exist")

	// Verify venv creation stage details
	assert.Equal(t, plan.PhaseAppDeps, venvStage.PhaseKey)
	assert.Len(t, venvStage.Operations, 1)

	execOp, ok := venvStage.Operations[0].(plan.Exec)
	require.True(t, ok)
	assert.Contains(t, execOp.Command, "uv venv /venv --python 3.11")
}

func TestUvBlock_DependencyInstallation_NonUvProject(t *testing.T) {
	composer := plan.NewPlanComposer()

	// Set up a python dependency
	composer.SetDependencies(map[string]*plan.Dependency{
		"python": {
			Name:            "python",
			ResolvedVersion: "3.11",
		},
	})

	// Add base stages
	setupBaseStages(t, composer)

	// Create config with packages
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Build: &config.Build{
			PythonVersion:  "3.11",
			PythonPackages: []string{"torch==2.0.1", "pandas==2.0.3"},
		},
	}
	err := cfg.ValidateAndComplete(tmpDir)
	require.NoError(t, err)

	src, err := project.NewSourceInfo(tmpDir, cfg)
	require.NoError(t, err)

	// Create and run the UV block
	block := &UvBlock{}
	err = block.Plan(t.Context(), src, composer)
	require.NoError(t, err)

	// Compose the plan
	composedPlan, err := composer.Compose()
	require.NoError(t, err)

	// Verify we have venv setup and sync stages
	venvStage := findStageByID(composedPlan.Stages, "uv-venv")
	require.NotNil(t, venvStage, "venv stage should exist")

	syncStage := findStageByID(composedPlan.Stages, "uv-sync")
	require.NotNil(t, syncStage, "sync stage should exist")
	assert.Equal(t, plan.PhaseAppDeps, syncStage.PhaseKey)

	// Sync stage should have: copy pyproject.toml + uv sync
	assert.Len(t, syncStage.Operations, 2)
	
	// Check that pyproject.toml copy is first
	copyOp, ok := syncStage.Operations[0].(plan.Copy)
	require.True(t, ok, "First operation should be Copy")
	assert.Equal(t, "uv-pyproject", copyOp.From.Local)
	
	// Check that uv sync is second
	execOp, ok := syncStage.Operations[1].(plan.Exec)
	require.True(t, ok, "Second operation should be Exec")
	assert.Contains(t, execOp.Command, "uv sync")
}

func TestUvBlock_DependencyInstallation_UvProject(t *testing.T) {
	composer := plan.NewPlanComposer()

	composer.SetDependencies(map[string]*plan.Dependency{
		"python": {Name: "python", ResolvedVersion: "3.11"},
	})

	setupBaseStages(t, composer)

	// Create a UV project with pyproject.toml
	tmpDir := t.TempDir()
	pyprojectPath := filepath.Join(tmpDir, "pyproject.toml")
	pyprojectContent := `[project]
name = "test-model"
version = "0.1.0"
dependencies = ["requests==2.31.0"]
`
	err := os.WriteFile(pyprojectPath, []byte(pyprojectContent), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		Build: &config.Build{
			PythonVersion: "3.11",
		},
	}
	err = cfg.ValidateAndComplete(tmpDir)
	require.NoError(t, err)

	src, err := project.NewSourceInfo(tmpDir, cfg)
	require.NoError(t, err)

	block := &UvBlock{}
	err = block.Plan(t.Context(), src, composer)
	require.NoError(t, err)

	composedPlan, err := composer.Compose()
	require.NoError(t, err)

	// Should have sync stage
	syncStage := findStageByID(composedPlan.Stages, "uv-sync")
	require.NotNil(t, syncStage)

	// Should copy pyproject.toml and run sync
	assert.Len(t, syncStage.Operations, 2)
	
	// Check copy operation
	copyOp, ok := syncStage.Operations[0].(plan.Copy)
	require.True(t, ok)
	assert.Equal(t, "source", copyOp.From.Local)
	assert.Contains(t, copyOp.Src, "pyproject.toml")
}

func TestUvBlock_GeneratedPyprojectContent(t *testing.T) {
	// Test the pyproject.toml generation
	packages := []string{"pandas==2.0.3", "requests==2.31.0"}
	cogWheelPath := "/mnt/wheel/cog-0.11.0-py3-none-any.whl"
	
	content := generatePyprojectToml(packages, cogWheelPath)
	
	// Verify structure
	assert.Contains(t, content, "[project]")
	assert.Contains(t, content, "name = \"cog-model\"")
	assert.Contains(t, content, "version = \"0.1.0\"")
	
	// Verify cog wheel is first dependency
	assert.Contains(t, content, "\"cog @ file:///mnt/wheel/cog-0.11.0-py3-none-any.whl\"")
	
	// Verify user packages are included
	assert.Contains(t, content, "\"pandas==2.0.3\"")
	assert.Contains(t, content, "\"requests==2.31.0\"")
}

func TestUvBlock_VenvCopyToRuntime(t *testing.T) {
	composer := plan.NewPlanComposer()

	composer.SetDependencies(map[string]*plan.Dependency{
		"python": {Name: "python", ResolvedVersion: "3.11"},
	})

	setupBaseStages(t, composer)

	tmpDir := t.TempDir()
	cfg := &config.Config{Build: &config.Build{PythonVersion: "3.11"}}
	err := cfg.ValidateAndComplete(tmpDir)
	require.NoError(t, err)

	src, err := project.NewSourceInfo(tmpDir, cfg)
	require.NoError(t, err)

	block := &UvBlock{}
	err = block.Plan(t.Context(), src, composer)
	require.NoError(t, err)

	composedPlan, err := composer.Compose()
	require.NoError(t, err)

	// Check venv copy stage
	copyStage := findStageByID(composedPlan.Stages, "copy-venv")
	require.NotNil(t, copyStage)
	assert.Equal(t, plan.ExportPhaseRuntime, copyStage.PhaseKey)
	assert.Len(t, copyStage.Operations, 2) // rm + copy

	// Check copy operation
	copyOp, ok := copyStage.Operations[1].(plan.Copy)
	require.True(t, ok)
	assert.Equal(t, []string{"/venv"}, copyOp.Src)
	assert.Equal(t, "/venv", copyOp.Dest)
}

// Helper functions for the tests
func setupBaseStages(t *testing.T, composer *plan.Composer) {
	buildBaseStage, err := composer.AddStage(plan.PhaseBase, "base", plan.WithSource(plan.FromImage("python:3.11")))
	require.NoError(t, err)
	buildBaseStage.AddOperation(plan.Exec{Command: "echo base"})

	exportBaseStage, err := composer.AddStage(plan.ExportPhaseBase, "export-base", plan.WithSource(plan.FromImage("python:3.11-slim")))
	require.NoError(t, err)
	exportBaseStage.AddOperation(plan.Exec{Command: "echo export-base"})
}

func findStageByID(stages []*plan.Stage, id string) *plan.Stage {
	for _, stage := range stages {
		if stage.ID == id {
			return stage
		}
	}
	return nil
}

