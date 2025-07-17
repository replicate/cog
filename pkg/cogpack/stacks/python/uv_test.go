package python

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/config"
)

func TestUvBlock_BasicPlanning(t *testing.T) {
	ctx := context.Background()
	composer := plan.NewPlanComposer()

	// Set up a python dependency
	composer.SetDependencies(map[string]*plan.Dependency{
		"python": {
			Name:            "python",
			ResolvedVersion: "3.11",
		},
	})

	// Create source info with temp directory
	tmpDir := t.TempDir()
	src, err := project.NewSourceInfo(tmpDir, &config.Config{})
	require.NoError(t, err)

	// Add a base stage so UV has something to build upon
	buildBaseStage, err := composer.AddStage(plan.PhaseBase, "base", plan.WithSource(plan.FromImage("python:3.11")))
	require.NoError(t, err)
	buildBaseStage.AddOperation(plan.Exec{Command: "echo base"})

	// Add a base export stage so UV export has something to build upon
	exportBaseComposerStage, err := composer.AddStage(plan.ExportPhaseBase, "export-base", plan.WithSource(plan.FromImage("python:3.11-slim")))
	require.NoError(t, err)
	exportBaseComposerStage.AddOperation(plan.Exec{Command: "echo export-base"})

	// Create and run the UV block
	block := &UvBlock{}
	err = block.Plan(ctx, src, composer)
	require.NoError(t, err)

	// Compose the plan
	composedPlan, err := composer.Compose()
	require.NoError(t, err)

	// Verify we have the expected stages (base + UV for both build and export)
	assert.Len(t, composedPlan.Stages, 4) // base, uv-venv, export-base, copy-venv

	// Find stages by their IDs and phase keys
	var baseStage, uvBuildStage, exportBaseStage, uvExportStage *plan.Stage
	for _, stage := range composedPlan.Stages {
		switch stage.ID {
		case "base":
			baseStage = stage
		case "uv-venv":
			uvBuildStage = stage
		case "export-base":
			exportBaseStage = stage
		case "copy-venv":
			uvExportStage = stage
		}
	}

	// Verify all stages were found
	require.NotNil(t, baseStage, "base stage not found")
	require.NotNil(t, uvBuildStage, "uv-venv stage not found")
	require.NotNil(t, exportBaseStage, "export-base stage not found")
	require.NotNil(t, uvExportStage, "copy-venv stage not found")

	// Verify stage phases
	assert.Equal(t, plan.PhaseBase, baseStage.PhaseKey)
	assert.Equal(t, plan.PhaseAppDeps, uvBuildStage.PhaseKey)
	assert.Equal(t, plan.ExportPhaseBase, exportBaseStage.PhaseKey)
	assert.Equal(t, plan.ExportPhaseRuntime, uvExportStage.PhaseKey)

	// Check UV build stage
	assert.Equal(t, "uv-venv", uvBuildStage.ID)
	assert.Len(t, uvBuildStage.Operations, 1)

	// Check UV export stage
	assert.Equal(t, "copy-venv", uvExportStage.ID)
	assert.Len(t, uvExportStage.Operations, 2)

	// Check that the Copy operation references the build stage
	copyOp, ok := uvExportStage.Operations[1].(plan.Copy)
	require.True(t, ok)
	assert.Equal(t, "uv-venv", copyOp.From.Stage)
	assert.Equal(t, plan.PhaseKey(""), copyOp.From.Phase) // Phase should be resolved
	assert.Equal(t, []string{"/venv"}, copyOp.Src)
	assert.Equal(t, "/venv", copyOp.Dest)
}
