package cogpack

import (
	"testing"

	_ "github.com/replicate/cog/pkg/cogpack/stacks" // register stacks
)

func TestBasicPythonProject(t *testing.T) {
	t.Skip("skipping until we have a basic python project")
	// // Create a mock source info for a basic Python project
	// // For now, we'll create a temporary directory for testing
	// tmpDir := t.TempDir()

	// // Create some test files
	// os.WriteFile(filepath.Join(tmpDir, "predict.py"), []byte("# test"), 0644)
	// os.WriteFile(filepath.Join(tmpDir, "requirements.txt"), []byte("torch==2.0.0"), 0644)

	// src, err := project.NewSourceInfo(tmpDir, &config.Config{
	// 	Build: &config.Build{
	// 		PythonVersion: "3.11",
	// 	},
	// })
	// if err != nil {
	// 	t.Fatalf("Failed to create SourceInfo: %v", err)
	// }
	// defer src.Close()

	// // Generate plan
	// result, err := GeneratePlan(context.Background(), src)
	// if err != nil {
	// 	t.Fatalf("GeneratePlan failed: %v", err)
	// }

	// // Verify basic structure
	// if result.Plan == nil {
	// 	t.Fatal("Plan is nil")
	// }

	// if result.Plan.Platform.OS != "linux" {
	// 	t.Errorf("Expected platform OS to be linux, got %s", result.Plan.Platform.OS)
	// }

	// if result.Plan.Platform.Arch != "amd64" {
	// 	t.Errorf("Expected platform Arch to be amd64, got %s", result.Plan.Platform.Arch)
	// }

	// // Verify Python dependency was resolved
	// pythonDep, exists := result.Plan.Dependencies["python"]
	// if !exists {
	// 	t.Fatal("Python dependency not found")
	// }

	// if pythonDep.ResolvedVersion == "" {
	// 	t.Error("Python dependency has no resolved version")
	// }

	// // Verify base image was selected
	// if result.Plan.BaseImage.Build == "" {
	// 	t.Error("No build base image selected")
	// }

	// if result.Plan.BaseImage.Runtime == "" {
	// 	t.Error("No runtime base image selected")
	// }

	// // Verify metadata
	// if result.Metadata.Stack != "python" {
	// 	t.Errorf("Expected stack to be python, got %s", result.Metadata.Stack)
	// }

	// // Print plan for debugging
	// planJSON, _ := json.MarshalIndent(result.Plan, "", "  ")
	// t.Logf("Generated plan:\n%s", planJSON)
}
