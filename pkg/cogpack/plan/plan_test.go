package plan

// func TestPlan_Phases(t *testing.T) {
// 	t.Run("add build phase", func(t *testing.T) {
// 		p := &Plan{}
// 		phase, err := p.addPhase(PhaseAppSource)
// 		require.NoError(t, err)
// 		assert.NotNil(t, phase)
// 		assert.Len(t, p.BuildPhases, 1)
// 		assert.Equal(t, phase, p.BuildPhases[0])
// 		assert.Contains(t, p.BuildPhases, phase)
// 		assert.Equal(t, p, phase.plan)
// 		assert.Equal(t, PhaseAppSource, phase.Name)
// 	})

// 	t.Run("add export phase", func(t *testing.T) {
// 		p := &Plan{}
// 		phase, err := p.addPhase(ExportPhaseApp)
// 		require.NoError(t, err)
// 		assert.NotNil(t, phase)
// 		assert.Len(t, p.ExportPhases, 1)
// 		assert.Equal(t, phase, p.ExportPhases[0])
// 		assert.Contains(t, p.ExportPhases, phase)
// 		assert.Equal(t, p, phase.plan)
// 		assert.Equal(t, ExportPhaseApp, phase.Name)
// 	})

// 	t.Run("add build phase with existing phase", func(t *testing.T) {
// 		p := &Plan{}
// 		phase, err := p.addPhase(PhaseSystemDeps)
// 		require.NoError(t, err)
// 		assert.NotNil(t, phase)
// 		assert.Len(t, p.BuildPhases, 1)

// 		phase, err = p.addPhase(PhaseSystemDeps)
// 		assert.ErrorIs(t, err, ErrDuplicatePhase)
// 		assert.Nil(t, phase)
// 		assert.Len(t, p.BuildPhases, 1)
// 	})

// 	t.Run("add export phase with existing phase", func(t *testing.T) {
// 		p := &Plan{}
// 		phase, err := p.addPhase(ExportPhaseBase)
// 		require.NoError(t, err)
// 		assert.NotNil(t, phase)
// 		assert.Len(t, p.ExportPhases, 1)

// 		phase, err = p.addPhase(ExportPhaseBase)
// 		assert.ErrorIs(t, err, ErrDuplicatePhase)
// 		assert.Nil(t, phase)
// 		assert.Len(t, p.ExportPhases, 1)
// 	})

// 	t.Run("get phase with existing phase", func(t *testing.T) {
// 		p := &Plan{}
// 		phase, err := p.addPhase(PhaseSystemDeps)
// 		require.NoError(t, err)
// 		assert.NotNil(t, phase)

// 		phase2 := p.getPhase(PhaseSystemDeps)
// 		assert.Equal(t, phase, phase2)
// 	})

// 	t.Run("get phase with non-existing phase", func(t *testing.T) {
// 		p := &Plan{}
// 		phase := p.getPhase(PhaseSystemDeps)
// 		assert.Nil(t, phase)
// 	})

// 	t.Run("get or create new phase", func(t *testing.T) {
// 		p := &Plan{}
// 		phase1 := p.getOrCreatePhase(PhaseSystemDeps)
// 		assert.Equal(t, PhaseSystemDeps, phase1.Name)
// 		assert.Len(t, p.BuildPhases, 1)

// 		phase2 := p.getOrCreatePhase(PhaseSystemDeps)
// 		assert.Equal(t, phase1, phase2)
// 		assert.Len(t, p.BuildPhases, 1)

// 		phase3 := p.getOrCreatePhase(PhaseFrameworkDeps)
// 		assert.Equal(t, PhaseFrameworkDeps, phase3.Name)
// 		assert.Len(t, p.BuildPhases, 2)
// 		assert.NotEqual(t, phase1, phase3)
// 		assert.Equal(t, p, phase3.plan)
// 	})
// }

// func TestPhaseAndStageTraversal(t *testing.T) {
// 	p := &Plan{}
// 	buildPhase1 := p.getOrCreatePhase(PhaseKey("build.phase1"))
// 	buildPhase1Stage1, err := p.AddStage(buildPhase1.Name, "build.phase1.stage1")
// 	require.NoError(t, err)
// 	buildPhase1Stage2, err := p.AddStage(buildPhase1.Name, "build.phase1.stage2")
// 	require.NoError(t, err)

// 	buildPhase2 := p.getOrCreatePhase(PhaseKey("build.phase2"))
// 	buildPhase2Stage1, err := p.AddStage(buildPhase2.Name, "build.phase2.stage1")
// 	require.NoError(t, err)
// 	buildPhase2Stage2, err := p.AddStage(buildPhase2.Name, "build.phase2.stage2")
// 	require.NoError(t, err)

// 	// an empty phase
// 	buildPhase3 := p.getOrCreatePhase(PhaseKey("build.phase3"))
// 	require.NoError(t, err)

// 	buildPhase4 := p.getOrCreatePhase(PhaseKey("build.phase4"))
// 	buildPhase4Stage1, err := p.AddStage(buildPhase4.Name, "build.phase4.stage1")
// 	require.NoError(t, err)

// 	exportPhase1 := p.getOrCreatePhase(PhaseKey("export.phase1"))
// 	exportPhase1Stage1, err := p.AddStage(exportPhase1.Name, "export.phase1.stage1")
// 	require.NoError(t, err)
// 	exportPhase1Stage2, err := p.AddStage(exportPhase1.Name, "export.phase1.stage2")
// 	require.NoError(t, err)

// 	exportPhase2 := p.getOrCreatePhase(PhaseKey("export.phase2"))
// 	exportPhase2Stage1, err := p.AddStage(exportPhase2.Name, "export.phase2.stage1")
// 	require.NoError(t, err)
// 	_, err = p.AddStage(exportPhase2.Name, "export.phase2.stage2")
// 	require.NoError(t, err)

// 	assert.NotNil(t, buildPhase1Stage1)
// 	assert.NotNil(t, buildPhase2Stage1)
// 	assert.NotNil(t, exportPhase1Stage1)
// 	assert.NotNil(t, exportPhase1Stage2)
// 	assert.NotNil(t, exportPhase2Stage1)

// 	t.Run("Plan.previousStage", func(t *testing.T) {
// 		cases := []struct {
// 			stage       *Stage
// 			expected    *Stage
// 			description string
// 		}{
// 			{
// 				stage:       buildPhase1Stage1,
// 				expected:    nil,
// 				description: "should return nil for first stage in first phase",
// 			},
// 			{
// 				stage:       buildPhase1Stage2,
// 				expected:    buildPhase1Stage1,
// 				description: "should returnprevious stage in same phase",
// 			},
// 			{
// 				stage:       buildPhase2Stage1,
// 				expected:    buildPhase1Stage2,
// 				description: "should return final stage in previous phase",
// 			},
// 			{
// 				stage:       buildPhase4Stage1,
// 				expected:    buildPhase2Stage2,
// 				description: "should return final stage in previous phase, skipping empty phase",
// 			},
// 			{
// 				stage:       exportPhase1Stage1,
// 				expected:    nil,
// 				description: "no previous stage for export.phase1.stage1",
// 			},
// 			{
// 				stage:       exportPhase1Stage2,
// 				expected:    exportPhase1Stage1,
// 				description: "previous stage in same phase",
// 			},
// 			{
// 				stage:       exportPhase2Stage1,
// 				expected:    exportPhase1Stage2,
// 				description: "should return final stage in previous phase",
// 			},
// 		}

// 		for _, tt := range cases {
// 			t.Run(tt.description, func(t *testing.T) {
// 				assert.Equal(t, tt.expected, p.previousStage(tt.stage), tt.description)
// 			})
// 		}
// 	})

// 	t.Run("Plan.previousPhase", func(t *testing.T) {
// 		cases := []struct {
// 			phase       *Phase
// 			expected    *Phase
// 			description string
// 		}{
// 			{
// 				phase:       buildPhase1,
// 				expected:    nil,
// 				description: "should return nil for first phase",
// 			},
// 			{
// 				phase:       buildPhase2,
// 				expected:    buildPhase1,
// 				description: "should return previous phase",
// 			},
// 			{
// 				phase:       buildPhase3,
// 				expected:    buildPhase2,
// 				description: "should return previous phase",
// 			},
// 			{
// 				phase:       buildPhase4,
// 				expected:    buildPhase3,
// 				description: "should return previous phase",
// 			},
// 			{
// 				phase:       exportPhase1,
// 				expected:    nil,
// 				description: "should return nil for first phase",
// 			},
// 			{
// 				phase:       exportPhase2,
// 				expected:    exportPhase1,
// 				description: "should return previous phase",
// 			},
// 		}

// 		for _, tt := range cases {
// 			t.Run(tt.description, func(t *testing.T) {
// 				assert.Equal(t, tt.expected, p.previousPhase(tt.phase), tt.description)
// 			})
// 		}
// 	})

// 	t.Run("Phase.finalStage", func(t *testing.T) {
// 		cases := []struct {
// 			phase       *Phase
// 			expected    *Stage
// 			description string
// 		}{
// 			{
// 				phase:       buildPhase1,
// 				expected:    buildPhase1Stage2,
// 				description: "should return last stage if phase has multiple stages",
// 			},
// 			{
// 				phase:       buildPhase3,
// 				expected:    nil,
// 				description: "should return nil if phase has no stages",
// 			},
// 			{
// 				phase:       buildPhase4,
// 				expected:    buildPhase4Stage1,
// 				description: "should return only stage if phase has one stage",
// 			},
// 		}

// 		for _, tt := range cases {
// 			t.Run(tt.description, func(t *testing.T) {
// 				assert.Equal(t, tt.expected, tt.phase.finalStage(), tt.description)
// 			})
// 		}
// 	})
// }

// func TestPlan_ResolveStageInputs(t *testing.T) {
// 	tests := []struct {
// 		name          string
// 		setup         func() (*Plan, *Stage)
// 		expectedInput Input
// 	}{
// 		{
// 			name: "resolve to previous stage in the same phase",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{}
// 				phase := p.getOrCreatePhase(PhaseKey("build.phase1"))
// 				_, err := p.AddStage(phase.Name, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				require.NoError(t, err)
// 				stage2, err := p.AddStage(phase.Name, "stage2")
// 				require.NoError(t, err)
// 				return p, stage2
// 			},
// 			expectedInput: Input{Auto: true, Stage: "stage1"},
// 		},
// 		// {
// 		// 	name: "resolve to previous phase result when stage is first in phase",
// 		// 	setup: func() (*Plan, *Stage) {
// 		// 		p := &Plan{}
// 		// 		phase1 := p.getOrCreatePhase(StagePhase("build.phase1"))
// 		// 		_, err := p.AddStage(phase1.Name, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 		// 		require.NoError(t, err)
// 		// 		phase2 := p.getOrCreatePhase(StagePhase("build.phase2"))
// 		// 		stage2, err := p.AddStage(phase2.Name, "stage2")
// 		// 		require.NoError(t, err)
// 		// 		return p, stage2
// 		// 	},
// 		// 	expectedInput: Input{Auto: true, Stage: "stage1"},
// 		// },
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			plan, stage := tt.setup()
// 			err := plan.resolveStageInputs()
// 			require.NoError(t, err)
// 			assert.Equal(t, tt.expectedInput, stage.Source)
// 		})
// 	}
// }

// func TestPlan_ResolveStageInput(t *testing.T) {
// 	tests := []struct {
// 		name          string
// 		setup         func() (*Plan, *Stage)
// 		expectedInput Input
// 	}{
// 		{
// 			name: "resolve to scratch when stage source is scratch",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{}
// 				phase := p.getOrCreatePhase(PhaseSystemDeps)
// 				stage, err := p.AddStage(phase.Name, "stage1", WithSource(FromScratch()))
// 				require.NoError(t, err)
// 				return p, stage
// 			},
// 			expectedInput: Input{Scratch: true},
// 		},
// 		{
// 			name: "resolve to image when image is provided",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{}
// 				phase := p.getOrCreatePhase(PhaseSystemDeps)
// 				stage, err := p.AddStage(phase.Name, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				require.NoError(t, err)
// 				return p, stage
// 			},
// 			expectedInput: Input{Image: "ubuntu:22.04"},
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			plan, stage := tt.setup()
// 			err := plan.resolveStageInputs()
// 			require.NoError(t, err)
// 			assert.Equal(t, tt.expectedInput, stage.Source)
// 		})
// 	}
// }

// func TestInput_Validate(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	tests := []struct {
// 		name    string
// 		input   Input
// 		wantErr bool
// 	}{
// 		{
// 			name:    "valid image input",
// 			input:   Input{Image: "ubuntu:20.04"},
// 			wantErr: false,
// 		},
// 		{
// 			name:    "valid stage input",
// 			input:   Input{Stage: "build-stage"},
// 			wantErr: false,
// 		},
// 		{
// 			name:    "valid local input",
// 			input:   Input{Local: "context-name"},
// 			wantErr: false,
// 		},
// 		{
// 			name:    "valid URL input",
// 			input:   Input{URL: "https://example.com/file.tar"},
// 			wantErr: false,
// 		},
// 		{
// 			name:    "valid phase input",
// 			input:   Input{Phase: PhaseSystemDeps},
// 			wantErr: false,
// 		},
// 		{
// 			name:    "valid auto input",
// 			input:   Input{Auto: true},
// 			wantErr: false,
// 		},
// 		{
// 			name:    "valid scratch input",
// 			input:   Input{Scratch: true},
// 			wantErr: false,
// 		},
// 		{
// 			name:    "no input sources",
// 			input:   Input{},
// 			wantErr: true,
// 		},
// 		{
// 			name: "multiple input sources - image and stage",
// 			input: Input{
// 				Image: "ubuntu:20.04",
// 				Stage: "build-stage",
// 			},
// 			wantErr: true,
// 		},
// 		{
// 			name: "multiple input sources - image and local",
// 			input: Input{
// 				Image: "ubuntu:20.04",
// 				Local: "context-name",
// 			},
// 			wantErr: true,
// 		},
// 		{
// 			name: "multiple input sources - stage and URL",
// 			input: Input{
// 				Stage: "build-stage",
// 				URL:   "https://example.com/file.tar",
// 			},
// 			wantErr: true,
// 		},
// 		{
// 			name: "multiple input sources - local and phase",
// 			input: Input{
// 				Local: "context-name",
// 				Phase: PhaseSystemDeps,
// 			},
// 			wantErr: true,
// 		},
// 		{
// 			name: "multiple input sources - auto and scratch",
// 			input: Input{
// 				Auto:    true,
// 				Scratch: true,
// 			},
// 			wantErr: true,
// 		},
// 		{
// 			name: "multiple input sources - image and auto",
// 			input: Input{
// 				Image: "ubuntu:20.04",
// 				Auto:  true,
// 			},
// 			wantErr: true,
// 		},
// 		{
// 			name: "multiple input sources - all fields",
// 			input: Input{
// 				Image:   "ubuntu:20.04",
// 				Stage:   "build-stage",
// 				Local:   "context-name",
// 				URL:     "https://example.com/file.tar",
// 				Phase:   PhaseSystemDeps,
// 				Auto:    true,
// 				Scratch: true,
// 			},
// 			wantErr: true,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			err := tt.input.Validate()
// 			if (err != nil) != tt.wantErr {
// 				t.Errorf("Input.Validate() error = %v, wantErr %v", err, tt.wantErr)
// 			}
// 		})
// 	}
// }

// func TestAutoInput_Resolution(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	tests := []struct {
// 		name          string
// 		setup         func() (*Plan, *Stage)
// 		expectedInput Input
// 		description   string
// 	}{
// 		{
// 			name: "auto resolves to previous stage in same phase",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add first stage to system-deps phase
// 				stage1, err := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				require.NoError(t, err)
// 				stage1.Operations = []Op{Exec{Command: "echo first"}}

// 				// Add second stage to same phase with Auto
// 				stage2, err := p.AddStage(PhaseSystemDeps, "stage2")
// 				require.NoError(t, err)
// 				stage2.Operations = []Op{Exec{Command: "echo second"}}

// 				return p, stage2
// 			},
// 			expectedInput: Input{Stage: "stage1"},
// 			description:   "Auto should resolve to previous stage in same phase",
// 		},
// 		{
// 			name: "auto resolves to previous phase result when first in phase",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add stages to system-deps phase
// 				stage1, err := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				require.NoError(t, err)
// 				stage1.Operations = []Op{Exec{Command: "echo first"}}

// 				stage2, err := p.AddStage(PhaseSystemDeps, "stage2")
// 				require.NoError(t, err)
// 				stage2.Operations = []Op{Exec{Command: "echo second"}}

// 				// Add first stage to runtime phase with Auto
// 				stage3, err := p.AddStage(PhaseRuntime, "stage3")
// 				require.NoError(t, err)
// 				stage3.Operations = []Op{Exec{Command: "echo runtime"}}

// 				return p, stage3
// 			},
// 			expectedInput: Input{Stage: "stage2"},
// 			description:   "Auto should resolve to previous phase result when first in phase",
// 		},
// 		{
// 			name: "auto in export phase resolves to previous export phase",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add stage to export-base phase
// 				stage1, err := p.AddStage(ExportPhaseBase, "export1", WithSource(FromImage("ubuntu:22.04")))
// 				require.NoError(t, err)
// 				stage1.Operations = []Op{Exec{Command: "echo export base"}}

// 				// Add stage to export-runtime phase with Auto
// 				stage2, err := p.AddStage(ExportPhaseRuntime, "export2")
// 				require.NoError(t, err)
// 				stage2.Operations = []Op{Exec{Command: "echo export runtime"}}

// 				return p, stage2
// 			},
// 			expectedInput: Input{Stage: "export1"},
// 			description:   "Auto should resolve to previous export phase result",
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			plan, stage := tt.setup()

// 			// Test that the stage was created with Auto input
// 			assert.Equal(t, Input{Auto: true}, stage.Source, "Stage should have Auto input")

// 			// Test that Auto resolves correctly (this would be done by the builder/translator)
// 			// For now, we can test the resolution logic by checking what the expected input should be
// 			expectedResult := tt.expectedInput

// 			// Verify the expected stage exists if it's a stage reference
// 			if expectedResult.Stage != "" {
// 				foundStage := plan.GetStage(expectedResult.Stage)
// 				assert.NotNil(t, foundStage, "Referenced stage should exist in plan")
// 			}
// 		})
// 	}
// }

// func TestResolveAutoInput(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	tests := []struct {
// 		name              string
// 		setup             func() *Plan
// 		currentPhase      StagePhase
// 		currentStageIndex int
// 		expectedInput     Input
// 	}{
// 		{
// 			name: "resolve to previous stage in same phase",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add two stages to system-deps phase
// 				stage1, err := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				require.NoError(t, err)
// 				stage1.Operations = []Op{Exec{Command: "echo first"}}

// 				stage2, err := p.AddStage(PhaseSystemDeps, "stage2", WithSource(FromCurrentState()))
// 				require.NoError(t, err)
// 				stage2.Operations = []Op{Exec{Command: "echo second"}}

// 				return p
// 			},
// 			currentPhase:      PhaseSystemDeps,
// 			currentStageIndex: 1, // Second stage (index 1)
// 			expectedInput:     Input{Stage: "stage1"},
// 		},
// 		{
// 			name: "resolve to previous phase result when first in phase",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add stage to system-deps phase
// 				stage1, err := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				require.NoError(t, err)
// 				stage1.Operations = []Op{Exec{Command: "echo first"}}

// 				// Add stage to runtime phase (will be first in phase)
// 				stage2, err := p.AddStage(PhaseRuntime, "stage2")
// 				require.NoError(t, err)
// 				stage2.Operations = []Op{Exec{Command: "echo runtime"}}

// 				return p
// 			},
// 			currentPhase:      PhaseRuntime,
// 			currentStageIndex: 0, // First stage in runtime phase
// 			expectedInput:     Input{Stage: "stage1"},
// 		},
// 		{
// 			name: "resolve to base image when no predecessor",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add stage to system-deps phase (first stage in first phase)
// 				stage1, err := p.AddStage(PhaseSystemDeps, "stage1")
// 				require.NoError(t, err)
// 				stage1.Operations = []Op{Exec{Command: "echo first"}}

// 				return p
// 			},
// 			currentPhase:      PhaseSystemDeps,
// 			currentStageIndex: 0, // First stage in first phase
// 			expectedInput:     Input{Image: "ubuntu:22.04"},
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			t.Skip("TODO: fix this test")
// 			// plan := tt.setup()

// 			// result := plan.ResolveAutoInput(tt.currentPhase, tt.currentStageIndex)

// 			// assert.Equal(t, tt.expectedInput, result, "Auto input should resolve correctly")
// 		})
// 	}
// }

// func TestResolveInput(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	p := &Plan{
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:22.04",
// 			Runtime: "ubuntu:22.04",
// 		},
// 	}

// 	// Add two stages for testing
// 	stage1, err := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 	require.NoError(t, err)
// 	stage1.Operations = []Op{Exec{Command: "echo first"}}

// 	stage2, err := p.AddStage(PhaseSystemDeps, "stage2")
// 	require.NoError(t, err)
// 	stage2.Operations = []Op{Exec{Command: "echo second"}}

// 	tests := []struct {
// 		name              string
// 		input             Input
// 		currentPhase      StagePhase
// 		currentStageIndex int
// 		expectedInput     Input
// 	}{
// 		{
// 			name:              "auto input resolves",
// 			input:             Input{Auto: true},
// 			currentPhase:      PhaseSystemDeps,
// 			currentStageIndex: 1, // Second stage should resolve to first stage
// 			expectedInput:     Input{Stage: "stage1"},
// 		},
// 		{
// 			name:              "scratch input returns as-is",
// 			input:             Input{Scratch: true},
// 			currentPhase:      PhaseSystemDeps,
// 			currentStageIndex: 0,
// 			expectedInput:     Input{Scratch: true},
// 		},
// 		{
// 			name:              "image input returns as-is",
// 			input:             Input{Image: "ubuntu:20.04"},
// 			currentPhase:      PhaseSystemDeps,
// 			currentStageIndex: 0,
// 			expectedInput:     Input{Image: "ubuntu:20.04"},
// 		},
// 		{
// 			name:              "stage input returns as-is",
// 			input:             Input{Stage: "some-stage"},
// 			currentPhase:      PhaseSystemDeps,
// 			currentStageIndex: 0,
// 			expectedInput:     Input{Stage: "some-stage"},
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			t.Skip("TODO: fix this test")
// 			// result := p.ResolveInput(tt.input, tt.currentPhase, tt.currentStageIndex)
// 			// assert.Equal(t, tt.expectedInput, result, "Input should resolve correctly")
// 		})
// 	}
// }

// func TestScratchInput_Behavior(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	tests := []struct {
// 		name        string
// 		setup       func() (*Plan, *Stage)
// 		description string
// 	}{
// 		{
// 			name: "scratch input creates scratch state",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add stage with scratch input
// 				stage, _ := p.AddStage(PhaseSystemDeps, "scratch-stage", WithSource(FromScratch()))
// 				stage.Operations = []Op{Exec{Command: "echo scratch"}}

// 				return p, stage
// 			},
// 			description: "Scratch input should create a scratch state",
// 		},
// 		{
// 			name: "scratch input in export phase",
// 			setup: func() (*Plan, *Stage) {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add stage with scratch input in export phase
// 				stage, err := p.AddStage(ExportPhaseBase, "export-scratch", WithSource(FromScratch()))
// 				require.NoError(t, err)
// 				stage.Operations = []Op{Exec{Command: "echo export scratch"}}

// 				return p, stage
// 			},
// 			description: "Scratch input should work in export phases",
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			plan, stage := tt.setup()

// 			// Test that the stage was created with Scratch input
// 			assert.Equal(t, Input{Scratch: true}, stage.Source, "Stage should have Scratch input")

// 			// Test that the plan validates correctly
// 			err := plan.Validate()
// 			assert.NoError(t, err, "Plan with scratch input should validate")
// 		})
// 	}
// }

// func TestPlanValidation_InputValidation(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	tests := []struct {
// 		name    string
// 		setup   func() *Plan
// 		wantErr bool
// 		errMsg  string
// 	}{
// 		{
// 			name: "plan validation calls input validation",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 					BuildPhases: []*Phase{
// 						{
// 							Name: PhaseSystemDeps,
// 							Stages: []*Stage{
// 								{
// 									ID:         "invalid-stage",
// 									Name:       "invalid",
// 									Source:     Input{}, // Invalid - no source specified
// 									Operations: []Op{Exec{Command: "echo test"}},
// 								},
// 							},
// 						},
// 					},
// 				}
// 				return p
// 			},
// 			wantErr: true,
// 			errMsg:  "exactly 1 input source is required",
// 		},
// 		{
// 			name: "plan validation passes with valid inputs",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 					BuildPhases: []*Phase{
// 						{
// 							Name: PhaseSystemDeps,
// 							Stages: []*Stage{
// 								{
// 									ID:         "valid-stage",
// 									Name:       "valid",
// 									Source:     Input{Image: "ubuntu:22.04"},
// 									Operations: []Op{Exec{Command: "echo test"}},
// 								},
// 							},
// 						},
// 					},
// 				}
// 				return p
// 			},
// 			wantErr: false,
// 		},
// 		{
// 			name: "plan validation passes with auto input",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 					BuildPhases: []*Phase{
// 						{
// 							Name: PhaseSystemDeps,
// 							Stages: []*Stage{
// 								{
// 									ID:         "auto-stage",
// 									Name:       "auto",
// 									Source:     Input{Auto: true},
// 									Operations: []Op{Exec{Command: "echo test"}},
// 								},
// 							},
// 						},
// 					},
// 				}
// 				return p
// 			},
// 			wantErr: false,
// 		},
// 		{
// 			name: "plan validation passes with scratch input",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 					BuildPhases: []*Phase{
// 						{
// 							Name: PhaseSystemDeps,
// 							Stages: []*Stage{
// 								{
// 									ID:         "scratch-stage",
// 									Name:       "scratch",
// 									Source:     Input{Scratch: true},
// 									Operations: []Op{Exec{Command: "echo test"}},
// 								},
// 							},
// 						},
// 					},
// 				}
// 				return p
// 			},
// 			wantErr: false,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			p := tt.setup()

// 			err := p.Validate()

// 			if tt.wantErr {
// 				assert.Error(t, err)
// 				if tt.errMsg != "" {
// 					assert.Contains(t, err.Error(), tt.errMsg)
// 				}
// 			} else {
// 				assert.NoError(t, err)
// 			}
// 		})
// 	}
// }

// func TestInput_IsEmpty(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	tests := []struct {
// 		name     string
// 		input    Input
// 		expected bool
// 	}{
// 		{
// 			name:     "empty input",
// 			input:    Input{},
// 			expected: true,
// 		},
// 		{
// 			name:     "image input not empty",
// 			input:    Input{Image: "ubuntu:20.04"},
// 			expected: false,
// 		},
// 		{
// 			name:     "stage input not empty",
// 			input:    Input{Stage: "stage1"},
// 			expected: false,
// 		},
// 		{
// 			name:     "local input not empty",
// 			input:    Input{Local: "context"},
// 			expected: false,
// 		},
// 		{
// 			name:     "URL input not empty",
// 			input:    Input{URL: "https://example.com/file.tar"},
// 			expected: false,
// 		},
// 		{
// 			name:     "phase input not empty",
// 			input:    Input{Phase: PhaseSystemDeps},
// 			expected: false,
// 		},
// 		{
// 			name:     "auto input not empty",
// 			input:    Input{Auto: true},
// 			expected: false,
// 		},
// 		{
// 			name:     "scratch input not empty",
// 			input:    Input{Scratch: true},
// 			expected: false,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			result := tt.input.IsEmpty()
// 			assert.Equal(t, tt.expected, result, "IsEmpty should return correct result")
// 		})
// 	}
// }
