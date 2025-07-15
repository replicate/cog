package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
)

func TestComposer_Phases(t *testing.T) {
	t.Run("add build phase", func(t *testing.T) {
		c := &PlanComposer{}
		phase := c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.buildPhases, 1)
		assert.Contains(t, c.buildPhases, phase)
		assert.Equal(t, c, phase.composer)
		assert.Equal(t, PhaseKey("build.phase"), phase.Name)
	})

	t.Run("add export phase", func(t *testing.T) {
		c := &PlanComposer{}
		phase := c.getOrCreatePhase(PhaseKey("export.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.exportPhases, 1)
		assert.Contains(t, c.exportPhases, phase)
		assert.Equal(t, c, phase.composer)
		assert.Equal(t, PhaseKey("export.phase"), phase.Name)
	})

	t.Run("add build phase with existing phase", func(t *testing.T) {
		c := &PlanComposer{}
		phase := c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.buildPhases, 1)
		assert.Contains(t, c.buildPhases, phase)

		phase = c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.Len(t, c.buildPhases, 1)
		assert.Contains(t, c.buildPhases, phase)
	})

	t.Run("add export phase with existing phase", func(t *testing.T) {
		c := &PlanComposer{}
		phase1 := c.getOrCreatePhase(PhaseKey("export.phase"))
		assert.NotNil(t, phase1)
		assert.Len(t, c.exportPhases, 1)
		assert.Contains(t, c.exportPhases, phase1)

		phase2 := c.getOrCreatePhase(PhaseKey("export.phase"))
		assert.Len(t, c.exportPhases, 1)
		assert.Equal(t, phase1, phase2)
	})

	t.Run("requires valid phase key", func(t *testing.T) {
		t.Skip("TODO: implement key validation, required to be prefixed with `build.` or `export.`")
	})

	// t.Run("get phase with existing phase", func(t *testing.T) {
	// 	p := &PlanComposer{}
	// 	phase := p.getOrCreatePhase(StagePhase("build.phase"))
	// 	assert.NotNil(t, phase)

	// 	phase2 := p.getPhase(StagePhase("build.phase"))
	// 	assert.Equal(t, phase, phase2)
	// })

	// t.Run("get phase with non-existing phase", func(t *testing.T) {
	// 	p := &Plan{}
	// 	phase := p.getPhase(PhaseSystemDeps)
	// 	assert.Nil(t, phase)
	// })
}

func TestComposer_Stages(t *testing.T) {
	t.Run("add stage is correctly added to a phase", func(t *testing.T) {
		c := NewPlanComposer()
		phase1 := c.getOrCreatePhase(PhaseSystemDeps)

		stage1, err := c.AddStage(phase1.Name, "stage1")
		require.NoError(t, err)
		require.NotNil(t, stage1)

		assert.Len(t, phase1.Stages, 1)
		assert.Contains(t, phase1.Stages, stage1)
		assert.Equal(t, c, stage1.GetComposer())
		assert.Equal(t, phase1, stage1.GetPhase())

		phase2 := c.getOrCreatePhase(PhaseAppSource)
		stage2, err := c.AddStage(phase2.Name, "stage2")
		require.NoError(t, err)
		require.NotNil(t, stage1)

		assert.Len(t, phase2.Stages, 1)
		assert.Contains(t, phase2.Stages, stage2)
		assert.Equal(t, c, stage2.GetComposer())
		assert.Equal(t, phase2, stage2.GetPhase())
		assert.NotContains(t, phase1.Stages, stage2)
	})

	t.Run("add stage fails with a duplicate stage id", func(t *testing.T) {
		c := NewPlanComposer()
		phase := c.getOrCreatePhase(PhaseSystemDeps)
		c.AddStage(phase.Name, "stage1")
		stage2, err := c.AddStage(phase.Name, "stage1")
		assert.ErrorIs(t, err, ErrDuplicateStageID)
		assert.Nil(t, stage2)
	})
}

func TestPlanComposer_BasicComposition(t *testing.T) {
	composer := NewPlanComposer()

	// Add a build stage
	stage1, err := composer.AddStage(PhaseSystemDeps, "install-deps", WithSource(FromImage("ubuntu:22.04")))
	require.NoError(t, err)
	stage1.AddOperation(Exec{Command: "apt-get update"})

	// Add another build stage with auto input
	stage2, err := composer.AddStage(PhaseRuntime, "install-python")
	require.NoError(t, err)
	stage2.AddOperation(Exec{Command: "apt-get install python3"})

	// Add export stage
	exportStage, err := composer.AddStage(ExportPhaseBase, "export-base", WithSource(FromImage("ubuntu:22.04-slim")))
	require.NoError(t, err)
	exportStage.AddOperation(Copy{
		From: Input{Stage: "install-python"},
		Src:  []string{"/usr/bin/python3"},
		Dest: "/usr/bin/",
	})

	// Compose the plan
	plan, err := composer.Compose()
	require.NoError(t, err)

	// Verify plan structure
	assert.Len(t, plan.BuildStages, 2)
	assert.Len(t, plan.ExportStages, 1)

	assert.Equal(t, "ubuntu:22.04", plan.BuildStages[0].Source.Image)
	assert.Equal(t, "install-deps", plan.BuildStages[1].Source.Stage)
	assert.Equal(t, "ubuntu:22.04-slim", plan.ExportStages[0].Source.Image)
}

func TestComposer_StageInputResolution(t *testing.T) {
	tests := []struct {
		name                  string
		setup                 func() *PlanComposer
		expectedBuildSources  []Input
		expectedExportSources []Input
	}{
		{
			name: "auto resolves to previous stage in same phase",
			setup: func() *PlanComposer {
				c := NewPlanComposer()

				stage1, _ := c.AddStage(PhaseKey("build.phase1"), "stage1", WithSource(FromImage("ubuntu:22.04")))
				stage1.AddOperation(Exec{Command: "echo first"})

				stage2, _ := c.AddStage(PhaseKey("build.phase1"), "stage2")
				stage2.AddOperation(Exec{Command: "echo second"})

				return c
			},
			expectedBuildSources: []Input{
				{Image: "ubuntu:22.04"},
				{Stage: "stage1"},
			},
			expectedExportSources: []Input{},
		},
		{
			name: "auto resolves to final stage of previous phase when first in phase",
			setup: func() *PlanComposer {
				c := NewPlanComposer()

				stage1, _ := c.AddStage(PhaseKey("build.phase1"), "stage1", WithSource(FromScratch()))
				stage1.AddOperation(Exec{Command: "echo first"})

				stage2, _ := c.AddStage(PhaseKey("build.phase1"), "stage2")
				stage2.AddOperation(Exec{Command: "echo second"})

				stage3, _ := c.AddStage(PhaseKey("build.phase2"), "stage3")
				stage3.AddOperation(Exec{Command: "echo third"})

				return c
			},
			expectedBuildSources: []Input{
				{Scratch: true},
				{Stage: "stage1"},
				{Stage: "stage2"},
			},
			expectedExportSources: []Input{},
		},
		{
			name: "auto skips empty phases when resolving a previous phase",
			setup: func() *PlanComposer {
				c := NewPlanComposer()

				stage1, _ := c.AddStage(PhaseKey("build.phase1"), "stage1", WithSource(FromScratch()))
				stage1.AddOperation(Exec{Command: "echo first"})

				stage2, _ := c.AddStage(PhaseKey("build.phase1"), "stage2")
				stage2.AddOperation(Exec{Command: "echo second"})

				c.getOrCreatePhase(PhaseKey("build.phase2"))

				stage3, _ := c.AddStage(PhaseKey("build.phase3"), "stage3")
				stage3.AddOperation(Exec{Command: "echo third"})

				return c
			},
			expectedBuildSources: []Input{
				{Scratch: true},
				{Stage: "stage1"},
				{Stage: "stage2"},
			},
			expectedExportSources: []Input{},
		},
		{
			name: "phase resolves to the last stage of the keyed phase",
			setup: func() *PlanComposer {
				c := NewPlanComposer()

				stage1, _ := c.AddStage(PhaseKey("build.phase1"), "stage1", WithSource(FromScratch()))
				stage1.AddOperation(Exec{Command: "echo first"})

				stage2, _ := c.AddStage(PhaseKey("build.phase1"), "stage2")
				stage2.AddOperation(Exec{Command: "echo second"})

				stage3, _ := c.AddStage(PhaseKey("build.phase2"), "stage3")
				stage3.AddOperation(Exec{Command: "echo third"})

				stage4, _ := c.AddStage(PhaseKey("build.phase3"), "stage4", WithSource(FromPhase(PhaseKey("build.phase1"))))
				stage4.AddOperation(Exec{Command: "echo fourth"})

				return c
			},
			expectedBuildSources: []Input{
				{Scratch: true},
				{Stage: "stage1"},
				{Stage: "stage2"},
				{Stage: "stage2"},
			},
			expectedExportSources: []Input{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			composer := tt.setup()
			plan, err := composer.Compose()
			require.NoError(t, err)

			actualSources := make([]Input, len(plan.BuildStages))
			for i, stage := range plan.BuildStages {
				actualSources[i] = stage.Source
			}
			assert.Equal(t, tt.expectedBuildSources, actualSources)

			actualSources = make([]Input, len(plan.ExportStages))
			for i, stage := range plan.ExportStages {
				actualSources[i] = stage.Source
			}
			assert.Equal(t, tt.expectedExportSources, actualSources)
		})
	}
}

func TestPlanComposer_StageAPIConvenience(t *testing.T) {
	composer := NewPlanComposer()

	stage, err := composer.AddStage(PhaseSystemDeps, "test-stage")
	require.NoError(t, err)

	// Test fluent API
	stage.
		AddOperation(Exec{Command: "echo one"}).
		AddOperation(Exec{Command: "echo two"}).
		SetEnv([]string{"FOO=bar"}).
		SetDir("/app").
		SetProvides("package1", "package2")

	// Verify
	assert.Len(t, stage.Operations, 2)
	assert.Equal(t, []string{"FOO=bar"}, stage.Env)
	assert.Equal(t, "/app", stage.Dir)
	assert.Equal(t, []string{"package1", "package2"}, stage.Provides)

	// Test bidirectional references
	assert.Equal(t, composer, stage.GetComposer())
	assert.Equal(t, PhaseSystemDeps, stage.GetPhase().Name)
}

func TestPlanComposer_ContextHandling(t *testing.T) {
	composer := NewPlanComposer()
	composer.SetBaseImage(&baseimg.BaseImage{
		Build:   "ubuntu:22.04",
		Runtime: "ubuntu:22.04",
	})

	// Add contexts
	composer.AddContext("wheel-context", &BuildContext{
		Name:        "wheel-context",
		SourceBlock: "cog-wheel",
		Description: "Cog wheel files",
	})

	// Create a stage that uses the context
	stage, err := composer.AddStage(PhaseAppDeps, "install-wheel", WithSource(FromScratch()))
	require.NoError(t, err)
	stage.AddOperation(Exec{
		Command: "pip install /mnt/wheel/*.whl",
		Mounts: []Mount{
			{
				Source: Input{Local: "wheel-context"},
				Target: "/mnt/wheel",
			},
		},
	})

	// Compose and verify
	plan, err := composer.Compose()
	require.NoError(t, err)

	assert.Contains(t, plan.Contexts, "wheel-context")
	assert.Equal(t, "Cog wheel files", plan.Contexts["wheel-context"].Description)
}

func TestPhaseAndStageTraversal(t *testing.T) {
	p := NewPlanComposer()
	buildPhase1 := p.getOrCreatePhase(PhaseKey("build.phase1"))
	buildPhase1Stage1, err := p.AddStage(buildPhase1.Name, "build.phase1.stage1")
	require.NoError(t, err)
	buildPhase1Stage2, err := p.AddStage(buildPhase1.Name, "build.phase1.stage2")
	require.NoError(t, err)

	buildPhase2 := p.getOrCreatePhase(PhaseKey("build.phase2"))
	buildPhase2Stage1, err := p.AddStage(buildPhase2.Name, "build.phase2.stage1")
	require.NoError(t, err)
	buildPhase2Stage2, err := p.AddStage(buildPhase2.Name, "build.phase2.stage2")
	require.NoError(t, err)

	// an empty phase
	buildPhase3 := p.getOrCreatePhase(PhaseKey("build.phase3"))
	require.NoError(t, err)

	buildPhase4 := p.getOrCreatePhase(PhaseKey("build.phase4"))
	buildPhase4Stage1, err := p.AddStage(buildPhase4.Name, "build.phase4.stage1")
	require.NoError(t, err)

	exportPhase1 := p.getOrCreatePhase(PhaseKey("export.phase1"))
	exportPhase1Stage1, err := p.AddStage(exportPhase1.Name, "export.phase1.stage1")
	require.NoError(t, err)
	exportPhase1Stage2, err := p.AddStage(exportPhase1.Name, "export.phase1.stage2")
	require.NoError(t, err)

	exportPhase2 := p.getOrCreatePhase(PhaseKey("export.phase2"))
	exportPhase2Stage1, err := p.AddStage(exportPhase2.Name, "export.phase2.stage1")
	require.NoError(t, err)
	_, err = p.AddStage(exportPhase2.Name, "export.phase2.stage2")
	require.NoError(t, err)

	t.Run("Plan.previousStage", func(t *testing.T) {
		cases := []struct {
			stage       *ComposerStage
			expected    *ComposerStage
			description string
		}{
			{
				stage:       buildPhase1Stage1,
				expected:    nil,
				description: "should return nil for first stage in first phase",
			},
			{
				stage:       buildPhase1Stage2,
				expected:    buildPhase1Stage1,
				description: "should returnprevious stage in same phase",
			},
			{
				stage:       buildPhase2Stage1,
				expected:    buildPhase1Stage2,
				description: "should return final stage in previous phase",
			},
			{
				stage:       buildPhase4Stage1,
				expected:    buildPhase2Stage2,
				description: "should return final stage in previous phase, skipping empty phase",
			},
			{
				stage:       exportPhase1Stage1,
				expected:    nil,
				description: "no previous stage for export.phase1.stage1",
			},
			{
				stage:       exportPhase1Stage2,
				expected:    exportPhase1Stage1,
				description: "previous stage in same phase",
			},
			{
				stage:       exportPhase2Stage1,
				expected:    exportPhase1Stage2,
				description: "should return final stage in previous phase",
			},
		}

		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				assert.Equal(t, tt.expected, p.previousStage(tt.stage), tt.description)
			})
		}
	})

	t.Run("Plan.previousPhase", func(t *testing.T) {
		cases := []struct {
			phase       *ComposerPhase
			expected    *ComposerPhase
			description string
		}{
			{
				phase:       buildPhase1,
				expected:    nil,
				description: "should return nil for first phase",
			},
			{
				phase:       buildPhase2,
				expected:    buildPhase1,
				description: "should return previous phase",
			},
			{
				phase:       buildPhase3,
				expected:    buildPhase2,
				description: "should return previous phase",
			},
			{
				phase:       buildPhase4,
				expected:    buildPhase3,
				description: "should return previous phase",
			},
			{
				phase:       exportPhase1,
				expected:    nil,
				description: "should return nil for first phase",
			},
			{
				phase:       exportPhase2,
				expected:    exportPhase1,
				description: "should return previous phase",
			},
		}

		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				assert.Equal(t, tt.expected, p.previousPhase(tt.phase), tt.description)
			})
		}
	})

	t.Run("Phase.lastStage", func(t *testing.T) {
		cases := []struct {
			phase       *ComposerPhase
			expected    *ComposerStage
			description string
		}{
			{
				phase:       buildPhase1,
				expected:    buildPhase1Stage2,
				description: "should return last stage if phase has multiple stages",
			},
			{
				phase:       buildPhase3,
				expected:    nil,
				description: "should return nil if phase has no stages",
			},
			{
				phase:       buildPhase4,
				expected:    buildPhase4Stage1,
				description: "should return only stage if phase has one stage",
			},
		}

		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.phase.lastStage(), tt.description)
			})
		}
	})
}
