package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
)

// newTestComposer creates a composer with simple phase names for testing
func newTestComposer(phases ...PhaseKey) *Composer {
	if len(phases) == 0 {
		// Use simple test phases if none provided
		phases = []PhaseKey{
			PhaseKey("build.1"),
			PhaseKey("build.2"),
			PhaseKey("build.3"),
			PhaseKey("export.1"),
			PhaseKey("export.2"),
		}
	}
	return newPlanComposerWithPhases(phases)
}

func TestComposer_Phases(t *testing.T) {
	t.Run("add build phase", func(t *testing.T) {
		c := &Composer{}
		phase := c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.buildPhases, 1)
		assert.Contains(t, c.buildPhases, phase)
		assert.Equal(t, c, phase.composer)
		assert.Equal(t, PhaseKey("build.phase"), phase.Key)
	})

	t.Run("add export phase", func(t *testing.T) {
		c := &Composer{}
		phase := c.getOrCreatePhase(PhaseKey("export.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.exportPhases, 1)
		assert.Contains(t, c.exportPhases, phase)
		assert.Equal(t, c, phase.composer)
		assert.Equal(t, PhaseKey("export.phase"), phase.Key)
	})

	t.Run("add build phase with existing phase", func(t *testing.T) {
		c := &Composer{}
		phase := c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.buildPhases, 1)
		assert.Contains(t, c.buildPhases, phase)

		phase = c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.Len(t, c.buildPhases, 1)
		assert.Contains(t, c.buildPhases, phase)
	})

	t.Run("add export phase with existing phase", func(t *testing.T) {
		c := &Composer{}
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

		stage1, err := c.AddStage(phase1.Key, "stage1")
		require.NoError(t, err)
		require.NotNil(t, stage1)

		assert.Len(t, phase1.Stages, 1)
		assert.Contains(t, phase1.Stages, stage1)
		assert.Equal(t, c, stage1.GetComposer())
		assert.Equal(t, phase1, stage1.GetPhase())

		phase2 := c.getOrCreatePhase(PhaseAppSource)
		stage2, err := c.AddStage(phase2.Key, "stage2")
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
		c.AddStage(phase.Key, "stage1")
		stage2, err := c.AddStage(phase.Key, "stage1")
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
		setup                 func() *Composer
		expectedBuildSources  []Input
		expectedExportSources []Input
	}{
		{
			name: "auto resolves to previous stage in same phase",
			setup: func() *Composer {
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
			setup: func() *Composer {
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
			setup: func() *Composer {
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
			setup: func() *Composer {
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
	assert.Equal(t, PhaseSystemDeps, stage.GetPhase().Key)
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
	p := newTestComposer(
		PhaseKey("build.phase1"), 
		PhaseKey("build.phase2"), 
		PhaseKey("build.phase3"), 
		PhaseKey("build.phase4"),
		PhaseKey("export.phase1"), 
		PhaseKey("export.phase2"),
	)
	buildPhase1 := p.getOrCreatePhase(PhaseKey("build.phase1"))
	buildPhase1Stage1, err := p.AddStage(buildPhase1.Key, "build.phase1.stage1")
	require.NoError(t, err)
	buildPhase1Stage2, err := p.AddStage(buildPhase1.Key, "build.phase1.stage2")
	require.NoError(t, err)

	buildPhase2 := p.getOrCreatePhase(PhaseKey("build.phase2"))
	buildPhase2Stage1, err := p.AddStage(buildPhase2.Key, "build.phase2.stage1")
	require.NoError(t, err)
	buildPhase2Stage2, err := p.AddStage(buildPhase2.Key, "build.phase2.stage2")
	require.NoError(t, err)

	// an empty phase
	buildPhase3 := p.getOrCreatePhase(PhaseKey("build.phase3"))
	require.NoError(t, err)

	buildPhase4 := p.getOrCreatePhase(PhaseKey("build.phase4"))
	buildPhase4Stage1, err := p.AddStage(buildPhase4.Key, "build.phase4.stage1")
	require.NoError(t, err)

	exportPhase1 := p.getOrCreatePhase(PhaseKey("export.phase1"))
	exportPhase1Stage1, err := p.AddStage(exportPhase1.Key, "export.phase1.stage1")
	require.NoError(t, err)
	exportPhase1Stage2, err := p.AddStage(exportPhase1.Key, "export.phase1.stage2")
	require.NoError(t, err)

	exportPhase2 := p.getOrCreatePhase(PhaseKey("export.phase2"))
	exportPhase2Stage1, err := p.AddStage(exportPhase2.Key, "export.phase2.stage1")
	require.NoError(t, err)
	_, err = p.AddStage(exportPhase2.Key, "export.phase2.stage2")
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

func TestComposer_ResolvePhaseStages(t *testing.T) {
	// Setup a complex phase/stage hierarchy for testing
	setupComposer := func() *Composer {
		p := NewPlanComposer()
		
		// Build phases
		buildPhase1 := p.getOrCreatePhase(PhaseSystemDeps)
		buildPhase1Stage1, _ := p.AddStage(buildPhase1.Key, "system-deps-1")
		buildPhase1Stage2, _ := p.AddStage(buildPhase1.Key, "system-deps-2")
		
		buildPhase2 := p.getOrCreatePhase(PhaseRuntime)
		buildPhase2Stage1, _ := p.AddStage(buildPhase2.Key, "runtime-1")
		
		// Empty build phase
		p.getOrCreatePhase(PhaseFrameworkDeps)
		
		buildPhase4 := p.getOrCreatePhase(PhaseAppDeps)
		buildPhase4Stage1, _ := p.AddStage(buildPhase4.Key, "app-deps-1")
		buildPhase4Stage2, _ := p.AddStage(buildPhase4.Key, "app-deps-2")
		
		// Another empty build phase
		p.getOrCreatePhase(PhaseAppSource)
		
		// Export phases
		exportPhase1 := p.getOrCreatePhase(ExportPhaseBase)
		exportPhase1Stage1, _ := p.AddStage(exportPhase1.Key, "export-base-1")
		
		// Empty export phase
		p.getOrCreatePhase(ExportPhaseRuntime)
		
		exportPhase3 := p.getOrCreatePhase(ExportPhaseApp)
		exportPhase3Stage1, _ := p.AddStage(exportPhase3.Key, "export-app-1")
		
		// Store references for easier test access
		p.SetDependencies(map[string]*Dependency{
			"test-buildPhase1Stage1": {Name: string(buildPhase1Stage1.ID)},
			"test-buildPhase1Stage2": {Name: string(buildPhase1Stage2.ID)},
			"test-buildPhase2Stage1": {Name: string(buildPhase2Stage1.ID)},
			"test-buildPhase4Stage1": {Name: string(buildPhase4Stage1.ID)},
			"test-buildPhase4Stage2": {Name: string(buildPhase4Stage2.ID)},
			"test-exportPhase1Stage1": {Name: string(exportPhase1Stage1.ID)},
			"test-exportPhase3Stage1": {Name: string(exportPhase3Stage1.ID)},
		})
		
		return p
	}

	t.Run("resolvePhaseInputStage", func(t *testing.T) {
		p := setupComposer()
		
		cases := []struct {
			phaseKey    PhaseKey
			expected    string // stage ID or empty for nil
			description string
		}{
			{
				phaseKey:    PhaseSystemDeps,
				expected:    "",
				description: "first phase should have no input stage",
			},
			{
				phaseKey:    PhaseRuntime,
				expected:    "system-deps-2",
				description: "should return last stage of previous phase",
			},
			{
				phaseKey:    PhaseFrameworkDeps,
				expected:    "runtime-1",
				description: "empty phase should return last stage of previous non-empty phase",
			},
			{
				phaseKey:    PhaseAppDeps,
				expected:    "runtime-1",
				description: "should skip empty phases when looking for input",
			},
			{
				phaseKey:    PhaseAppSource,
				expected:    "app-deps-2",
				description: "empty phase at end should return last stage of previous non-empty phase",
			},
			{
				phaseKey:    ExportPhaseBase,
				expected:    "",
				description: "first export phase should have no input stage",
			},
			{
				phaseKey:    ExportPhaseRuntime,
				expected:    "export-base-1",
				description: "empty export phase should return last stage of previous export phase",
			},
			{
				phaseKey:    ExportPhaseApp,
				expected:    "export-base-1",
				description: "should skip empty export phases when looking for input",
			},
		}
		
		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				phase := p.findComposerPhase(tt.phaseKey)
				require.NotNil(t, phase, "phase %s should exist", tt.phaseKey)
				
				result := p.resolvePhaseInputStage(phase)
				if tt.expected == "" {
					assert.Nil(t, result, tt.description)
				} else {
					require.NotNil(t, result, tt.description)
					assert.Equal(t, tt.expected, result.ID, tt.description)
				}
			})
		}
	})

	t.Run("resolvePhaseOutputStage", func(t *testing.T) {
		p := setupComposer()
		
		cases := []struct {
			phaseKey    PhaseKey
			expected    string // stage ID or empty for nil
			description string
		}{
			{
				phaseKey:    PhaseSystemDeps,
				expected:    "system-deps-2",
				description: "should return last stage of phase with stages",
			},
			{
				phaseKey:    PhaseRuntime,
				expected:    "runtime-1",
				description: "should return last stage of phase with single stage",
			},
			{
				phaseKey:    PhaseFrameworkDeps,
				expected:    "runtime-1",
				description: "empty phase should walk back to find last stage",
			},
			{
				phaseKey:    PhaseAppDeps,
				expected:    "app-deps-2",
				description: "should return last stage of phase after empty phase",
			},
			{
				phaseKey:    PhaseAppSource,
				expected:    "app-deps-2",
				description: "empty phase at end should walk back to find last stage",
			},
			{
				phaseKey:    ExportPhaseBase,
				expected:    "export-base-1",
				description: "should return stage from export phase",
			},
			{
				phaseKey:    ExportPhaseRuntime,
				expected:    "export-base-1",
				description: "empty export phase should walk back to find last stage",
			},
			{
				phaseKey:    ExportPhaseApp,
				expected:    "export-app-1",
				description: "should return stage from export phase after empty phase",
			},
		}
		
		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				phase := p.findComposerPhase(tt.phaseKey)
				require.NotNil(t, phase, "phase %s should exist", tt.phaseKey)
				
				result := p.resolvePhaseOutputStage(phase)
				if tt.expected == "" {
					assert.Nil(t, result, tt.description)
				} else {
					require.NotNil(t, result, tt.description)
					assert.Equal(t, tt.expected, result.ID, tt.description)
				}
			})
		}
	})

	t.Run("resolvePhaseOutputStage with all empty phases", func(t *testing.T) {
		p := NewPlanComposer()
		
		// Create only empty phases
		p.getOrCreatePhase(PhaseSystemDeps)
		p.getOrCreatePhase(PhaseRuntime)
		phase := p.getOrCreatePhase(PhaseAppDeps)
		
		result := p.resolvePhaseOutputStage(phase)
		assert.Nil(t, result, "should return nil when all phases are empty")
	})

	t.Run("edge cases", func(t *testing.T) {
		t.Run("nil phase", func(t *testing.T) {
			p := NewPlanComposer()
			
			// These functions should handle nil gracefully
			assert.Nil(t, p.resolvePhaseInputStage(nil))
			assert.Nil(t, p.resolvePhaseOutputStage(nil))
		})
		
		t.Run("phase not in composer", func(t *testing.T) {
			p := NewPlanComposer()
			orphanPhase := &ComposerPhase{
				Key:      PhaseSystemDeps,
				Stages:   []*ComposerStage{},
				composer: p,
			}
			
			// Should handle gracefully
			assert.Nil(t, p.resolvePhaseInputStage(orphanPhase))
			assert.Nil(t, p.resolvePhaseOutputStage(orphanPhase))
		})
	})
}

func TestComposer_OperationInputResolution(t *testing.T) {
	t.Run("Copy operation with phase reference", func(t *testing.T) {
		p := NewPlanComposer()
		
		// Build phase with venv
		buildStage, err := p.AddStage(PhaseAppDeps, "create-venv", WithSource(FromImage("python:3.11")))
		require.NoError(t, err)
		buildStage.AddOperation(Exec{Command: "python -m venv /venv"})
		
		// Add a phase to reference as the "final build output"
		p.getOrCreatePhase(PhaseKey("build.final"))
		
		// Export phase that copies from the build
		exportStage, err := p.AddStage(ExportPhaseRuntime, "copy-venv", WithSource(FromImage("python:3.11-slim")))
		require.NoError(t, err)
		exportStage.AddOperation(Copy{
			From: Input{Phase: PhaseKey("build.final")},
			Src:  []string{"/venv"},
			Dest: "/venv",
		})
		
		// Compose the plan
		plan, err := p.Compose()
		require.NoError(t, err)
		
		// The Copy operation should have its From input resolved
		require.Len(t, plan.ExportStages, 1)
		require.Len(t, plan.ExportStages[0].Operations, 1)
		
		copyOp, ok := plan.ExportStages[0].Operations[0].(Copy)
		require.True(t, ok, "operation should be a Copy")
		
		// Phase reference should be resolved to the create-venv stage
		assert.Equal(t, "create-venv", copyOp.From.Stage)
		assert.Equal(t, PhaseKey(""), copyOp.From.Phase)
	})
	
	t.Run("Multiple operations with different input types", func(t *testing.T) {
		p := NewPlanComposer()
		
		// Build stages
		stage1, err := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
		require.NoError(t, err)
		stage1.AddOperation(Exec{Command: "echo stage1"})
		
		stage2, err := p.AddStage(PhaseRuntime, "stage2")
		require.NoError(t, err)
		stage2.AddOperation(Exec{Command: "echo stage2"})
		
		// Export stage with multiple copy operations
		exportStage, err := p.AddStage(ExportPhaseBase, "export", WithSource(FromImage("ubuntu:22.04-slim")))
		require.NoError(t, err)
		exportStage.AddOperations(
			Copy{
				From: Input{Stage: "stage1"},
				Src:  []string{"/file1"},
				Dest: "/file1",
			},
			Copy{
				From: Input{Phase: PhaseRuntime},
				Src:  []string{"/file2"},
				Dest: "/file2",
			},
			Copy{
				From: Input{Local: "context1"},
				Src:  []string{"/file3"},
				Dest: "/file3",
			},
		)
		
		// Compose the plan
		plan, err := p.Compose()
		require.NoError(t, err)
		
		// Check all operations were resolved correctly
		require.Len(t, plan.ExportStages[0].Operations, 3)
		
		copy1 := plan.ExportStages[0].Operations[0].(Copy)
		assert.Equal(t, "stage1", copy1.From.Stage)
		
		copy2 := plan.ExportStages[0].Operations[1].(Copy)
		assert.Equal(t, "stage2", copy2.From.Stage)
		assert.Equal(t, PhaseKey(""), copy2.From.Phase)
		
		copy3 := plan.ExportStages[0].Operations[2].(Copy)
		assert.Equal(t, "context1", copy3.From.Local)
	})
	
	t.Run("Operation with invalid phase reference", func(t *testing.T) {
		p := NewPlanComposer()
		
		// Build stage
		buildStage, err := p.AddStage(PhaseSystemDeps, "build", WithSource(FromImage("ubuntu:22.04")))
		require.NoError(t, err)
		buildStage.AddOperation(Exec{Command: "echo build"})
		
		// Export stage with copy from non-existent phase
		exportStage, err := p.AddStage(ExportPhaseBase, "export", WithSource(FromImage("ubuntu:22.04-slim")))
		require.NoError(t, err)
		exportStage.AddOperation(Copy{
			From: Input{Phase: PhaseKey("build.nonexistent")},
			Src:  []string{"/file"},
			Dest: "/file",
		})
		
		// Compose should fail
		_, err = p.Compose()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")
	})
}
