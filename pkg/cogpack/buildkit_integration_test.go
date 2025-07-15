package cogpack_test

// // Integration test that generates a plan and builds it into a Docker image
// // using the BuildKitBuilder. Skipped unless environment variable
// // COGPACK_INTEGRATION is set (to prevent failures on systems without Docker).
// func TestBuildKitIntegration_BasicModel(t *testing.T) {
// 	if os.Getenv("COGPACK_INTEGRATION") == "" {
// 		t.Skip("set COGPACK_INTEGRATION=1 to run integration build test")
// 	}

// 	modelDir := "testdata/basicpython"

// 	os.Setenv("COGPACK", "1")

// 	tag := "cogpack-test:basic"

// 	// Create Docker command
// 	ctx := t.Context()
// 	dockerCmd, err := docker.NewAPIClient(ctx)
// 	if err != nil {
// 		t.Fatalf("create docker client: %v", err)
// 	}

// 	// Builder factory that creates BuildKit builder
// 	builderFactory := func(cmd command.Command) builder.Builder {
// 		return builder.NewBuildKitBuilder(cmd)
// 	}

// 	// Build using the new API
// 	plan, err := cogpack.BuildWithDocker(ctx, modelDir, tag, dockerCmd, builderFactory)
// 	if err != nil {
// 		t.Fatalf("build with docker: %v", err)
// 	}

// 	if plan == nil {
// 		t.Fatal("plan should not be nil")
// 	}

// 	t.Logf("Build completed successfully, plan has %d build phases and %d export phases",
// 		len(plan.BuildPhases), len(plan.ExportPhases))

// 	// Debug: Print plan structure
// 	for i, phase := range plan.BuildPhases {
// 		t.Logf("Build Phase %d: %s (%d stages)", i, phase.Name, len(phase.Stages))
// 		for j, stage := range phase.Stages {
// 			t.Logf("  Stage %d: %s (ID: %s)", j, stage.Name, stage.ID)
// 		}
// 	}

// 	for i, phase := range plan.ExportPhases {
// 		t.Logf("Export Phase %d: %s (%d stages)", i, phase.Name, len(phase.Stages))
// 		for j, stage := range phase.Stages {
// 			t.Logf("  Stage %d: %s (ID: %s)", j, stage.Name, stage.ID)
// 		}
// 	}

// 	// List all images to debug
// 	listCmd := exec.Command("docker", "image", "ls", "--format", "table {{.Repository}}:{{.Tag}}")
// 	if out, err := listCmd.Output(); err == nil {
// 		t.Logf("Current images:\n%s", string(out))
// 	}

// 	// Give a moment for the image to be fully committed to Docker daemon
// 	time.Sleep(100 * time.Millisecond)

// 	// Try multiple ways to verify the image exists
// 	// First try: docker image inspect
// 	if err := exec.Command("docker", "image", "inspect", tag).Run(); err != nil {
// 		t.Logf("docker image inspect failed: %v", err)

// 		// Second try: docker images grep
// 		grepCmd := exec.Command("sh", "-c", "docker images | grep cogpack-test")
// 		if out, err := grepCmd.Output(); err == nil {
// 			t.Logf("Images matching cogpack-test:\n%s", string(out))
// 		}

// 		// Third try: check if we can run the image
// 		runCmd := exec.Command("docker", "run", "--rm", tag, "echo", "test")
// 		if err := runCmd.Run(); err != nil {
// 			t.Fatalf("image %s is not usable: %v", tag, err)
// 		} else {
// 			t.Logf("Image %s is runnable even though inspect failed", tag)
// 		}
// 	} else {
// 		t.Logf("Image %s verified successfully", tag)
// 	}
// }
