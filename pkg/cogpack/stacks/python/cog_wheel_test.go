package python

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

func TestCogWheelBlock_Name(t *testing.T) {
	block := &CogWheelBlock{}
	assert.Equal(t, "cog-wheel", block.Name())
}

func TestCogWheelBlock_Detect(t *testing.T) {
	block := &CogWheelBlock{}
	ctx := context.Background()
	src := &project.SourceInfo{}

	// CogWheelBlock should always detect as true
	detected, err := block.Detect(ctx, src)
	require.NoError(t, err)
	assert.True(t, detected)
}

func TestCogWheelBlock_Dependencies(t *testing.T) {
	block := &CogWheelBlock{}
	ctx := context.Background()
	src := &project.SourceInfo{}

	// CogWheelBlock should not emit any dependencies
	deps, err := block.Dependencies(ctx, src)
	require.NoError(t, err)
	assert.Nil(t, deps)
}

func TestCogWheelBlock_Plan(t *testing.T) {
	block := &CogWheelBlock{}
	ctx := context.Background()
	src := &project.SourceInfo{}

	// Create a test plan
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		BaseImage: &baseimg.BaseImage{
			Build:   "ubuntu:20.04",
			Runtime: "ubuntu:20.04",
			Metadata: baseimg.BaseImageMetadata{
				Packages: map[string]baseimg.Package{},
			},
		},
		BuildPhases:  []*plan.Phase{},
		ExportPhases: []*plan.Phase{},
	}

	// Execute the plan
	err := block.Plan(ctx, src, p)
	require.NoError(t, err)

	// Check that stage was added
	stage := p.GetStage("cog-wheel")
	require.NotNil(t, stage)
	assert.Equal(t, "cog-wheel", stage.ID)
	assert.Equal(t, "cog-wheel", stage.Name)

	// Check that stage has the expected operations
	require.Len(t, stage.Operations, 1)
	
	exec, ok := stage.Operations[0].(plan.Exec)
	require.True(t, ok)
	assert.Contains(t, exec.Command, "uv pip install")
	assert.Contains(t, exec.Command, "/mnt/wheel/embed/*.whl")
	assert.Contains(t, exec.Command, "pydantic>=1.9,<3")

	// Check that stage has the expected mounts
	require.Len(t, exec.Mounts, 1)
	mount := exec.Mounts[0]
	assert.Equal(t, "wheel-context", mount.Source.Local)
	assert.Equal(t, "/mnt/wheel", mount.Target)

	// Check that wheel context was added to plan
	require.Contains(t, p.Contexts, "wheel-context")
	wheelContext := p.Contexts["wheel-context"]
	assert.Equal(t, "wheel-context", wheelContext.Name)
	assert.Equal(t, "cog-wheel", wheelContext.SourceBlock)
	assert.Equal(t, "Cog wheel file for installation", wheelContext.Description)
	assert.Equal(t, "embedded-wheel", wheelContext.Metadata["type"])
	assert.NotNil(t, wheelContext.FS)
}

func TestCogWheelBlock_Plan_Integration(t *testing.T) {
	block := &CogWheelBlock{}
	ctx := context.Background()
	src := &project.SourceInfo{}

	// Create a more realistic plan with existing phases
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		BaseImage: &baseimg.BaseImage{
			Build:   "ubuntu:20.04",
			Runtime: "ubuntu:20.04",
			Metadata: baseimg.BaseImageMetadata{
				Packages: map[string]baseimg.Package{},
			},
		},
		BuildPhases:  []*plan.Phase{},
		ExportPhases: []*plan.Phase{},
	}

	// Add a preceding stage in the correct phase (PhaseFrameworkDeps is predecessor to PhaseAppDeps)
	precedingStage, err := p.AddStage(plan.PhaseFrameworkDeps, "framework-setup", "framework-setup")
	require.NoError(t, err)
	precedingStage.Operations = []plan.Op{
		plan.Exec{Command: "echo 'framework setup complete'"},
	}

	// Execute the CogWheelBlock plan
	err = block.Plan(ctx, src, p)
	require.NoError(t, err)

	// Check that cog-wheel stage was added to the correct phase
	var appDepsPhase *plan.Phase
	for _, phase := range p.BuildPhases {
		if phase.Name == plan.PhaseAppDeps {
			appDepsPhase = phase
			break
		}
	}
	require.NotNil(t, appDepsPhase)
	assert.Equal(t, plan.PhaseAppDeps, appDepsPhase.Name)

	// Check that the stage exists in the correct phase
	require.Len(t, appDepsPhase.Stages, 1)
	cogWheelStage := appDepsPhase.Stages[0]
	assert.Equal(t, "cog-wheel", cogWheelStage.ID)

	// Check stage input is resolved from preceding phase
	frameworkPhaseResult := p.GetPhaseResult(plan.PhaseFrameworkDeps)
	assert.Equal(t, "framework-setup", frameworkPhaseResult.Stage)
	assert.Equal(t, frameworkPhaseResult, cogWheelStage.Source)
}