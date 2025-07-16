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
	baseStage, err := composer.AddStage(plan.PhaseBase, "base", plan.WithSource(plan.FromImage("python:3.11")))
	require.NoError(t, err)
	baseStage.AddOperation(plan.Exec{Command: "echo base"})
	
	// Add a base export stage so UV export has something to build upon
	exportBaseStage, err := composer.AddStage(plan.ExportPhaseBase, "export-base", plan.WithSource(plan.FromImage("python:3.11-slim")))
	require.NoError(t, err)
	exportBaseStage.AddOperation(plan.Exec{Command: "echo export-base"})
	
	// Create and run the UV block
	block := &UvBlock{}
	err = block.Plan(ctx, src, composer)
	require.NoError(t, err)
	
	// Compose the plan
	composedPlan, err := composer.Compose()
	require.NoError(t, err)
	
	// Verify we have the expected stages (base + UV)
	assert.Len(t, composedPlan.BuildStages, 2)
	assert.Len(t, composedPlan.ExportStages, 2)
	
	// Check UV build stage
	uvBuildStage := composedPlan.BuildStages[1]
	assert.Equal(t, "uv-venv", uvBuildStage.ID)
	assert.Len(t, uvBuildStage.Operations, 1)
	
	// Check UV export stage
	uvExportStage := composedPlan.ExportStages[1]
	assert.Equal(t, "copy-venv", uvExportStage.ID)
	assert.Len(t, uvExportStage.Operations, 1)
	
	// Check that the Copy operation references the build stage
	copyOp, ok := uvExportStage.Operations[0].(plan.Copy)
	require.True(t, ok)
	assert.Equal(t, "uv-venv", copyOp.From.Stage)
	assert.Equal(t, plan.PhaseKey(""), copyOp.From.Phase) // Phase should be resolved
	assert.Equal(t, []string{"/venv"}, copyOp.Src)
	assert.Equal(t, "/venv", copyOp.Dest)
}