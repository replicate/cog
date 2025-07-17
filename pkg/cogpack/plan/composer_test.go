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
		c := &Composer{phases: []*ComposerPhase{}}
		phase := c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.phases, 1)
		assert.Contains(t, c.phases, phase)
		assert.Equal(t, c, phase.composer)
		assert.Equal(t, PhaseKey("build.phase"), phase.Key)
	})

	t.Run("add export phase", func(t *testing.T) {
		c := &Composer{phases: []*ComposerPhase{}}
		phase := c.getOrCreatePhase(PhaseKey("export.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.phases, 1)
		assert.Contains(t, c.phases, phase)
		assert.Equal(t, c, phase.composer)
		assert.Equal(t, PhaseKey("export.phase"), phase.Key)
	})

	t.Run("add build phase with existing phase", func(t *testing.T) {
		c := &Composer{phases: []*ComposerPhase{}}
		phase := c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.NotNil(t, phase)
		assert.Len(t, c.phases, 1)
		assert.Contains(t, c.phases, phase)

		phase = c.getOrCreatePhase(PhaseKey("build.phase"))
		assert.Len(t, c.phases, 1)
		assert.Contains(t, c.phases, phase)
	})

	t.Run("add export phase with existing phase", func(t *testing.T) {
		c := &Composer{phases: []*ComposerPhase{}}
		phase1 := c.getOrCreatePhase(PhaseKey("export.phase"))
		assert.NotNil(t, phase1)
		assert.Len(t, c.phases, 1)
		assert.Contains(t, c.phases, phase1)

		phase2 := c.getOrCreatePhase(PhaseKey("export.phase"))
		assert.Len(t, c.phases, 1)
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
	assert.Len(t, plan.Stages, 3)

	assert.Equal(t, "ubuntu:22.04", plan.Stages[0].Source.Image)
	assert.Equal(t, "install-deps", plan.Stages[1].Source.Stage)
	assert.Equal(t, "ubuntu:22.04-slim", plan.Stages[2].Source.Image)
}

func TestComposer_StageInputResolution(t *testing.T) {
	tests := []struct {
		name           string
		setup          func() *Composer
		expectedInputs []Input
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
			expectedInputs: []Input{
				{Image: "ubuntu:22.04"},
				{Stage: "stage1"},
			},
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
			expectedInputs: []Input{
				{Scratch: true},
				{Stage: "stage1"},
				{Stage: "stage2"},
			},
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
			expectedInputs: []Input{
				{Scratch: true},
				{Stage: "stage1"},
				{Stage: "stage2"},
			},
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
			expectedInputs: []Input{
				{Scratch: true},
				{Stage: "stage1"},
				{Stage: "stage2"},
				{Stage: "stage2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			composer := tt.setup()
			plan, err := composer.Compose()
			require.NoError(t, err)

			var actualSources []Input
			for _, stage := range plan.Stages {
				actualSources = append(actualSources, stage.Source)
			}
			assert.Equal(t, tt.expectedInputs, actualSources)
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

func TestComposer_ResolvingInputAndOutput(t *testing.T) {
	p := newTestComposer(
		PhaseKey("build.phase0"),
		PhaseKey("build.phase1"),
		PhaseKey("build.phase2"),
		PhaseKey("build.phase3"),
		PhaseKey("build.phase4"),
		PhaseKey("export.phase1"),
		PhaseKey("export.phase2"),
	)
	// an empty phase at the beginning
	buildPhase0 := p.getOrCreatePhase(PhaseKey("build.phase0"))

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

	// an empty phase in the middle
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
	exportPhase2Stage2, err := p.AddStage(exportPhase2.Key, "export.phase2.stage2")
	require.NoError(t, err)

	t.Run("resolveStageInputStage", func(t *testing.T) {
		cases := []struct {
			stage         *ComposerStage
			expectedStage *ComposerStage
			expectedError error
			description   string
		}{
			{
				stage:         buildPhase1Stage1,
				expectedError: ErrNoInputStage,
				description:   "should return nil for first stage with no previous stage",
			},
			{
				stage:         buildPhase1Stage2,
				expectedStage: buildPhase1Stage1,
				description:   "should returnprevious stage in same phase",
			},
			{
				stage:         buildPhase2Stage1,
				expectedStage: buildPhase1Stage2,
				description:   "should return final stage in previous phase",
			},
			{
				stage:         buildPhase4Stage1,
				expectedStage: buildPhase2Stage2,
				description:   "should return final stage in previous phase, skipping empty phase",
			},
			{
				stage:         exportPhase1Stage1,
				expectedStage: buildPhase4Stage1,
				description:   "export phase can reference previous build phase stage",
			},
			{
				stage:         exportPhase1Stage2,
				expectedStage: exportPhase1Stage1,
				description:   "previous stage in same phase",
			},
			{
				stage:         exportPhase2Stage1,
				expectedStage: exportPhase1Stage2,
				description:   "should return final stage in previous phase",
			},
			{
				stage:         &ComposerStage{ID: "unregistered-stage"},
				expectedError: ErrStageNotFound,
				description:   "should return error for unregistered stage",
			},
		}

		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				inputStage, err := p.resolveStageInputStage(tt.stage)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
				} else {
					require.NoError(t, err)
				}
				if tt.expectedStage != nil {
					assert.Equal(t, tt.expectedStage, inputStage, tt.description)
				} else {
					assert.Nil(t, inputStage, tt.description)
				}
			})
		}
	})

	t.Run("resolvePhaseInputStage", func(t *testing.T) {
		cases := []struct {
			phase         *ComposerPhase
			expectedStage *ComposerStage
			expectedError error
			description   string
		}{
			{
				phase:         buildPhase0,
				expectedError: ErrNoInputStage,
				description:   "should return ErrNoInputStage for first phase",
			},
			{
				phase:         buildPhase1,
				expectedError: ErrNoInputStage,
				description:   "should return ErrNoInputStage for 2nd phase with no previous stages",
			},
			{
				phase:         buildPhase2,
				expectedStage: buildPhase1Stage2,
				description:   "should return last stage of previous phase",
			},
			{
				phase:         buildPhase3,
				expectedStage: buildPhase2Stage2,
				description:   "should return last stage of previous phase",
			},
			{
				phase:         buildPhase4,
				expectedStage: buildPhase2Stage2,
				description:   "should return last stage of previous phase, skipping empty phase",
			},
			{
				phase:         exportPhase1,
				expectedStage: buildPhase4Stage1,
				description:   "export phase can reference previous build phase stage",
			},
			{
				phase:         exportPhase2,
				expectedStage: exportPhase1Stage2,
				description:   "should return last stage of previous phase",
			},
			{
				phase:         &ComposerPhase{Key: PhaseKey("unregistered-phase")},
				expectedError: ErrPhaseNotFound,
				description:   "should return error for unregistered phase",
			},
		}

		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				inputStage, err := p.resolvePhaseInputStage(tt.phase)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
				} else {
					require.NoError(t, err)
				}
				if tt.expectedStage != nil {
					assert.Equal(t, tt.expectedStage, inputStage, tt.description)
				} else {
					assert.Nil(t, inputStage, tt.description)
				}
			})
		}
	})

	t.Run("resolvePhaseOutputStage", func(t *testing.T) {
		cases := []struct {
			phase         *ComposerPhase
			expected      *ComposerStage
			expectedError error
			description   string
		}{
			{
				phase:       buildPhase1,
				expected:    buildPhase1Stage2,
				description: "should return last stage of current phase",
			},
			{
				phase:       buildPhase2,
				expected:    buildPhase2Stage2,
				description: "should return last stage of current phase",
			},
			{
				phase:       buildPhase3,
				expected:    buildPhase2Stage2,
				description: "should return last stage of previous phase if current phase is empty",
			},
			{
				phase:       buildPhase4,
				expected:    buildPhase4Stage1,
				description: "should return last stage of current phase",
			},
			{
				phase:       exportPhase1,
				expected:    exportPhase1Stage2,
				description: "should return last stage of current phase",
			},
			{
				phase:       exportPhase2,
				expected:    exportPhase2Stage2,
				description: "should return last stage of current phase",
			},
			{
				phase:         &ComposerPhase{Key: PhaseKey("unregistered-phase")},
				expected:      nil,
				expectedError: ErrPhaseNotFound,
				description:   "should return PhaseNotFound error for unregistered phase",
			},
		}

		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				outputStage, err := p.resolvePhaseOutputStage(tt.phase)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
				} else {
					require.NoError(t, err)
				}
				if tt.expected != nil {
					assert.Equal(t, tt.expected, outputStage, tt.description)
				} else {
					assert.Nil(t, outputStage, tt.description)
				}
			})
		}
	})

	t.Run("resolveInputFromStage", func(t *testing.T) {
		cases := []struct {
			fromStage     *ComposerStage
			input         Input
			resolvedInput *Input
			expectedError error
			description   string
		}{
			{
				fromStage:     buildPhase2Stage1,
				input:         Input{Phase: PhaseKey("build.phase1")},
				resolvedInput: &Input{Stage: "build.phase1.stage2"},
				description:   "should return final stage of target phase",
			},
			{
				fromStage:     buildPhase4Stage1,
				input:         Input{Phase: PhaseKey("build.phase0")},
				expectedError: ErrNoInputStage,
				description:   "should return error if target phase has no output stages",
			},
			{
				fromStage:     buildPhase2Stage1,
				input:         Input{Phase: PhaseKey("does.not.exist")},
				expectedError: ErrPhaseNotFound,
				description:   "should return error when target phase is unregistered",
			},
			{
				fromStage:     buildPhase2Stage1,
				input:         Input{Stage: "build.phase1.stage1"},
				resolvedInput: &Input{Stage: "build.phase1.stage1"},
				description:   "should return target stage",
			},
			{
				fromStage:     buildPhase2Stage1,
				input:         Input{Stage: "does.not.exist"},
				expectedError: ErrStageNotFound,
				description:   "should return error when target stage is unregistered",
			},
			{
				fromStage:     buildPhase2Stage2,
				input:         Input{Auto: true},
				resolvedInput: &Input{Stage: "build.phase2.stage1"},
				description:   "should return previous stage when target is auto",
			},
			{
				fromStage:     buildPhase2Stage2,
				input:         Input{Scratch: true},
				resolvedInput: &Input{Scratch: true},
				description:   "should return scratch unaltered ",
			},
			{
				fromStage:     buildPhase2Stage2,
				input:         Input{Local: "context1"},
				resolvedInput: &Input{Local: "context1"},
				description:   "should return local input unaltered",
			},
			{
				fromStage:     buildPhase2Stage2,
				input:         Input{URL: "https://example.com/file.txt"},
				resolvedInput: &Input{URL: "https://example.com/file.txt"},
				description:   "should return URL input unaltered",
			},
			{
				fromStage:     &ComposerStage{ID: "does.not.exist"},
				input:         Input{Scratch: true},
				expectedError: ErrStageNotFound,
				description:   "should return error when resolving input from unregistered stage",
			},
		}

		for _, tt := range cases {
			t.Run(tt.description, func(t *testing.T) {
				resolvedInput, err := p.resolveInputFromStage(tt.input, tt.fromStage)
				if tt.expectedError != nil {
					assert.ErrorIs(t, err, tt.expectedError)
				} else {
					require.NoError(t, err)
				}
				assert.Equal(t, tt.resolvedInput, resolvedInput, tt.description)
			})
		}

	})
}

func TestComposer_OperationInputResolution(t *testing.T) {
	t.Run("Copy operation with phase reference", func(t *testing.T) {
		p := NewPlanComposer()

		// Build phase with venv
		buildStage, err := p.AddStage(PhaseAppDeps, "create-venv", WithSource(FromImage("python:3.11")))
		require.NoError(t, err)
		buildStage.AddOperation(Exec{Command: "python -m venv /venv"})

		// Export phase that copies from the build using PhaseBuildComplete
		exportStage, err := p.AddStage(ExportPhaseRuntime, "copy-venv", WithSource(FromImage("python:3.11-slim")))
		require.NoError(t, err)
		exportStage.AddOperation(Copy{
			From: Input{Phase: PhaseBuildComplete},
			Src:  []string{"/venv"},
			Dest: "/venv",
		})

		// Compose the plan
		plan, err := p.Compose()
		require.NoError(t, err)

		// The Copy operation should have its From input resolved

		require.Len(t, plan.Stages, 2)
		require.Len(t, plan.Stages[1].Operations, 1)

		copyOp, ok := plan.Stages[1].Operations[0].(Copy)
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
		require.Len(t, plan.Stages[2].Operations, 3)

		copy1 := plan.Stages[2].Operations[0].(Copy)
		assert.Equal(t, "stage1", copy1.From.Stage)

		copy2 := plan.Stages[2].Operations[1].(Copy)
		assert.Equal(t, "stage2", copy2.From.Stage)
		assert.Equal(t, PhaseKey(""), copy2.From.Phase)

		copy3 := plan.Stages[2].Operations[2].(Copy)
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
		assert.ErrorIs(t, err, ErrPhaseNotFound)
	})
}
