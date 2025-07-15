package builder

// func TestCogWheelBlock_Integration_WithMounts(t *testing.T) {

// 	// Create a CogWheelBlock
// 	block := &python.CogWheelBlock{}

// 	// Create a realistic plan
// 	p := &plan.Plan{
// 		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:20.04",
// 			Runtime: "ubuntu:20.04",
// 			Metadata: baseimg.BaseImageMetadata{
// 				Packages: map[string]baseimg.Package{
// 					"python": {
// 						Name:       "python",
// 						Version:    "3.11.8",
// 						Source:     "base-image",
// 						Executable: "/usr/bin/python3",
// 					},
// 				},
// 			},
// 		},
// 		BuildPhases:  []*plan.Phase{},
// 		ExportPhases: []*plan.Phase{},
// 		Contexts:     make(map[string]*plan.BuildContext),
// 	}

// 	// Add preceding stages to establish proper phase chain
// 	baseStage, err := p.AddStage(plan.PhaseBase, "base-setup", plan.WithName("Base Setup"))
// 	require.NoError(t, err)
// 	baseStage.Operations = []plan.Op{
// 		plan.Exec{Command: "apt-get update"},
// 	}

// 	runtimeStage, err := p.AddStage(plan.PhaseRuntime, "python-runtime", plan.WithName("Python Runtime"))
// 	require.NoError(t, err)
// 	runtimeStage.Operations = []plan.Op{
// 		plan.Exec{Command: "python3 --version"},
// 	}

// 	frameworkStage, err := p.AddStage(plan.PhaseFrameworkDeps, "framework-deps", plan.WithName("Framework Deps"))
// 	require.NoError(t, err)
// 	frameworkStage.Operations = []plan.Op{
// 		plan.Exec{Command: "echo 'framework setup complete'"},
// 	}

// 	// Execute the CogWheelBlock plan
// 	src := &project.SourceInfo{}
// 	err = block.Plan(t.Context(), src, p)
// 	require.NoError(t, err)

// 	// Verify the plan structure
// 	cogWheelStage := p.GetStage("cog-wheel")
// 	require.NotNil(t, cogWheelStage)

// 	// Verify the exec operation with mount
// 	require.Len(t, cogWheelStage.Operations, 1)
// 	exec, ok := cogWheelStage.Operations[0].(plan.Exec)
// 	require.True(t, ok)

// 	// Check command
// 	assert.Contains(t, exec.Command, "uv pip install")
// 	assert.Contains(t, exec.Command, "/mnt/wheel/embed/*.whl")
// 	assert.Contains(t, exec.Command, "pydantic")

// 	// Check mounts
// 	require.Len(t, exec.Mounts, 1)
// 	mount := exec.Mounts[0]
// 	assert.Equal(t, "wheel-context", mount.Source.Local)
// 	assert.Equal(t, "/mnt/wheel", mount.Target)

// 	// Test LLB translation with the mount
// 	_, stageStates, err := translatePlan(t.Context(), p)
// 	require.NoError(t, err)

// 	// Verify all stages are translated
// 	assert.Contains(t, stageStates, "base-setup")
// 	assert.Contains(t, stageStates, "python-runtime")
// 	assert.Contains(t, stageStates, "framework-deps")
// 	assert.Contains(t, stageStates, "cog-wheel")
// }

// func TestWheelContext_Integration_WithBuildKit(t *testing.T) {
// 	// Test that the generic context system works with temporary directories
// 	tempDir := t.TempDir()

// 	// Create context from directory
// 	wheelContext, err := NewContextFromDirectory("wheel-context", tempDir)
// 	require.NoError(t, err)
// 	defer wheelContext.Close()

// 	// Verify wheel context properties
// 	assert.Equal(t, "wheel-context", wheelContext.Name())
// 	assert.NotNil(t, wheelContext.FS())

// 	// Test fs.FS interface
// 	fs := wheelContext.FS()
// 	assert.NotNil(t, fs)

// 	// This would be used in BuildKit's LocalMounts
// 	// We can't test BuildKit integration without Docker, but we can verify
// 	// that the context is properly structured
// }

// func TestEndToEnd_MountBasedWheelInstallation(t *testing.T) {
// 	ctx := context.Background()

// 	// Create complete build scenario
// 	p := &plan.Plan{
// 		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
// 		Dependencies: map[string]*plan.Dependency{
// 			"python": {
// 				Name:             "python",
// 				Provider:         "base-image",
// 				RequestedVersion: "3.11",
// 				ResolvedVersion:  "3.11.8",
// 				Source:           "base-image",
// 			},
// 		},
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:20.04",
// 			Runtime: "ubuntu:20.04",
// 			Metadata: baseimg.BaseImageMetadata{
// 				Packages: map[string]baseimg.Package{
// 					"python": {
// 						Name:       "python",
// 						Version:    "3.11.8",
// 						Source:     "base-image",
// 						Executable: "/usr/bin/python3",
// 					},
// 				},
// 			},
// 		},
// 		BuildPhases:  []*plan.Phase{},
// 		ExportPhases: []*plan.Phase{},
// 		Contexts:     make(map[string]*plan.BuildContext),
// 	}

// 	// Simulate a complete Python stack build
// 	uvBlock := &python.UvBlock{}
// 	cogWheelBlock := &python.CogWheelBlock{}

// 	src := &project.SourceInfo{}

// 	// Add UV installation
// 	err := uvBlock.Plan(ctx, src, p)
// 	require.NoError(t, err)

// 	// Add cog wheel installation
// 	err = cogWheelBlock.Plan(ctx, src, p)
// 	require.NoError(t, err)

// 	// Verify plan can be translated to LLB
// 	_, stageStates, err := translatePlan(ctx, p)
// 	require.NoError(t, err)

// 	// Verify both stages are present
// 	assert.Contains(t, stageStates, "uv-venv")
// 	assert.Contains(t, stageStates, "cog-wheel")

// 	// Verify cog-wheel stage has mount
// 	cogWheelStage := p.GetStage("cog-wheel")
// 	require.NotNil(t, cogWheelStage)

// 	exec, ok := cogWheelStage.Operations[0].(plan.Exec)
// 	require.True(t, ok)
// 	require.Len(t, exec.Mounts, 1)

// 	mount := exec.Mounts[0]
// 	assert.Equal(t, "wheel-context", mount.Source.Local)
// 	assert.Equal(t, "/mnt/wheel", mount.Target)

// 	// The mount should reference the wheel-context that BuildKit will provide
// 	assert.Contains(t, exec.Command, "/mnt/wheel/embed/*.whl")
// }
