package plan

// func TestValidatePlan_Complete(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	// Create a valid plan
// 	p := &Plan{
// 		Platform: Platform{OS: "linux", Arch: "amd64"},
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:20.04",
// 			Runtime: "ubuntu:20.04",
// 			Metadata: baseimg.BaseImageMetadata{
// 				Packages: map[string]baseimg.Package{},
// 			},
// 		},
// 		Dependencies: map[string]*Dependency{},
// 		BuildPhases:  []*Phase{},
// 		ExportPhases: []*Phase{},
// 		Contexts:     map[string]*BuildContext{},
// 	}

// 	// Should pass validation
// 	err := ValidatePlan(p)
// 	assert.NoError(t, err)

// 	// Also test the method interface
// 	err = p.Validate()
// 	assert.NoError(t, err)
// }

// func TestValidatePlan_MissingBaseImage(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	p := &Plan{
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "", // Missing
// 			Runtime: "ubuntu:20.04",
// 		},
// 	}

// 	err := ValidatePlan(p)
// 	assert.Error(t, err)
// 	assert.Contains(t, err.Error(), "no build base image specified")
// }

// func TestValidatePlan_DuplicateStageID(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	p := &Plan{
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:20.04",
// 			Runtime: "ubuntu:20.04",
// 		},
// 		BuildPhases: []*Phase{
// 			{
// 				Name: PhaseBase,
// 				Stages: []*Stage{
// 					{ID: "duplicate-id", Name: "stage1"},
// 					{ID: "duplicate-id", Name: "stage2"}, // Duplicate ID
// 				},
// 			},
// 		},
// 	}

// 	err := ValidatePlan(p)
// 	assert.Error(t, err)
// 	assert.Contains(t, err.Error(), "duplicate stage ID")
// }

// func TestValidatePlan_MissingContext(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	p := &Plan{
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:20.04",
// 			Runtime: "ubuntu:20.04",
// 		},
// 		BuildPhases: []*Phase{
// 			{
// 				Name: PhaseBase,
// 				Stages: []*Stage{
// 					{
// 						ID:     "test-stage",
// 						Name:   "test",
// 						Source: Input{Image: "ubuntu:20.04"}, // valid source
// 						Operations: []Op{
// 							Exec{
// 								Command: "echo test",
// 								Mounts: []Mount{
// 									{
// 										Source: Input{Local: "missing-context"},
// 										Target: "/mnt/test",
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 		Contexts: map[string]*BuildContext{}, // Empty - missing context
// 	}

// 	err := ValidatePlan(p)
// 	assert.Error(t, err)
// 	assert.Contains(t, err.Error(), "context \"missing-context\" referenced but not defined")
// }

// func TestValidatePlan_ValidContext(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	p := &Plan{
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:20.04",
// 			Runtime: "ubuntu:20.04",
// 		},
// 		BuildPhases: []*Phase{
// 			{
// 				Name: PhaseBase,
// 				Stages: []*Stage{
// 					{
// 						ID:     "test-stage",
// 						Name:   "test",
// 						Source: Input{Image: "ubuntu:20.04"}, // valid source
// 						Operations: []Op{
// 							Exec{
// 								Command: "echo test",
// 								Mounts: []Mount{
// 									{
// 										Source: Input{Local: "valid-context"},
// 										Target: "/mnt/test",
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 		Contexts: map[string]*BuildContext{
// 			"valid-context": {
// 				Name:        "valid-context",
// 				Description: "test context",
// 			},
// 		},
// 	}

// 	err := ValidatePlan(p)
// 	assert.NoError(t, err)
// }

// func TestValidatePlan_CopyWithMissingContext(t *testing.T) {
// 	t.Skip("TODO: fix this test")
// 	p := &Plan{
// 		BaseImage: &baseimg.BaseImage{
// 			Build:   "ubuntu:20.04",
// 			Runtime: "ubuntu:20.04",
// 		},
// 		BuildPhases: []*Phase{
// 			{
// 				Name: PhaseBase,
// 				Stages: []*Stage{
// 					{
// 						ID:     "test-stage",
// 						Name:   "test",
// 						Source: Input{Image: "ubuntu:20.04"}, // valid source
// 						Operations: []Op{
// 							Copy{
// 								From: Input{Local: "missing-context"},
// 								Src:  []string{"file.txt"},
// 								Dest: "/app/file.txt",
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 		Contexts: map[string]*BuildContext{}, // Empty - missing context
// 	}

// 	err := ValidatePlan(p)
// 	assert.Error(t, err)
// 	assert.Contains(t, err.Error(), "context \"missing-context\" referenced but not defined")
// }
