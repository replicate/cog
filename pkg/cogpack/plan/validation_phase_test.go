package plan

// func TestValidatePlan_PhaseInput(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	tests := []struct {
// 		name    string
// 		setup   func() *Plan
// 		wantErr bool
// 		errMsg  string
// 	}{
// 		{
// 			name: "valid phase reference",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add a stage to system-deps phase
// 				stage1, _ := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				stage1.Operations = []Op{Exec{Command: "echo hello"}}

// 				// Add a stage that references the phase
// 				stage2, _ := p.AddStage(PhaseRuntime, "stage2", WithSource(FromPhase(PhaseSystemDeps)))
// 				stage2.Operations = []Op{Exec{Command: "echo world"}}

// 				return p
// 			},
// 			wantErr: false,
// 		},
// 		{
// 			name: "phase reference with no stages",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add a stage that references a phase with no stages
// 				stage1, _ := p.AddStage(PhaseRuntime, "stage1", WithSource(FromPhase(PhaseSystemDeps)))
// 				stage1.Operations = []Op{Exec{Command: "echo hello"}}

// 				return p
// 			},
// 			wantErr: true,
// 			errMsg:  "phase \"system-deps\" has no stages or does not exist",
// 		},
// 		{
// 			name: "phase reference in mount",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 					Contexts: map[string]*BuildContext{
// 						"test-context": {
// 							Name: "test-context",
// 						},
// 					},
// 				}

// 				// Add a stage to system-deps phase
// 				stage1, _ := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				stage1.Operations = []Op{Exec{Command: "echo hello"}}

// 				// Add a stage with a mount that references the phase
// 				stage2, _ := p.AddStage(PhaseRuntime, "stage2", WithSource(FromCurrentState()))
// 				stage2.Operations = []Op{
// 					Exec{
// 						Command: "ls /mnt",
// 						Mounts: []Mount{
// 							{
// 								Source: Input{Phase: PhaseSystemDeps},
// 								Target: "/mnt/phase",
// 							},
// 						},
// 					},
// 				}

// 				return p
// 			},
// 			wantErr: false,
// 		},
// 		{
// 			name: "copy from phase",
// 			setup: func() *Plan {
// 				p := &Plan{
// 					BaseImage: &baseimg.BaseImage{
// 						Build:   "ubuntu:22.04",
// 						Runtime: "ubuntu:22.04",
// 					},
// 				}

// 				// Add stages to system-deps phase
// 				stage1, _ := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromImage("ubuntu:22.04")))
// 				stage1.Operations = []Op{Exec{Command: "echo hello > /file.txt"}}

// 				stage2, _ := p.AddStage(PhaseSystemDeps, "stage2", WithSource(FromCurrentState()))
// 				stage2.Operations = []Op{Exec{Command: "echo world > /file2.txt"}}

// 				// Add a stage that copies from the phase (should use last stage)
// 				stage3, _ := p.AddStage(PhaseRuntime, "stage3", WithSource(FromCurrentState()))
// 				stage3.Operations = []Op{
// 					Copy{
// 						From: Input{Phase: PhaseSystemDeps},
// 						Src:  []string{"/file2.txt"},
// 						Dest: "/app/file2.txt",
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

// func TestGetPhaseResult(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	p := &Plan{
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:22.04",
// 			Runtime: "ubuntu:22.04",
// 		},
// 	}

// 	// Test empty phase
// 	result := p.GetPhaseResult(PhaseSystemDeps)
// 	assert.Equal(t, Input{}, result)

// 	// Add a stage to the phase
// 	stage1, err := p.AddStage(PhaseSystemDeps, "stage1", WithSource(FromCurrentState()))
// 	require.NoError(t, err)
// 	stage1.Operations = []Op{Exec{Command: "echo hello"}}

// 	// Should return input referencing the stage
// 	result = p.GetPhaseResult(PhaseSystemDeps)
// 	assert.Equal(t, Input{Stage: "stage1"}, result)

// 	// Add another stage to the same phase
// 	stage2, err := p.AddStage(PhaseSystemDeps, "stage2", WithSource(FromCurrentState()))
// 	require.NoError(t, err)
// 	stage2.Operations = []Op{Exec{Command: "echo world"}}

// 	// Should return input referencing the last stage
// 	result = p.GetPhaseResult(PhaseSystemDeps)
// 	assert.Equal(t, Input{Stage: "stage2"}, result)

// 	// Test export phase
// 	exportStage, err := p.AddStage(ExportPhaseRuntime, "export1", WithSource(FromCurrentState()))
// 	require.NoError(t, err)
// 	exportStage.Operations = []Op{Exec{Command: "echo export"}}

// 	result = p.GetPhaseResult(ExportPhaseRuntime)
// 	assert.Equal(t, Input{Stage: "export1"}, result)
// }
