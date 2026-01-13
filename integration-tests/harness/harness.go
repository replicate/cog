// Package harness provides utilities for running cog integration tests.
package harness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rogpeppe/go-internal/testscript"
)

// Harness provides utilities for running cog integration tests.
type Harness struct {
	CogBinary string
	// realHome is captured at creation time before testscript overrides HOME
	realHome string
}

// New creates a new Harness, resolving the cog binary location.
func New() (*Harness, error) {
	cogBinary, err := ResolveCogBinary()
	if err != nil {
		return nil, err
	}
	return &Harness{
		CogBinary: cogBinary,
		realHome:  os.Getenv("HOME"),
	}, nil
}

// ResolveCogBinary finds the cog binary to use for tests.
// It checks (in order):
// 1. COG_BINARY environment variable
// 2. Build from source (if in cog repository)
func ResolveCogBinary() (string, error) {
	if cogBinary := os.Getenv("COG_BINARY"); cogBinary != "" {
		if !filepath.IsAbs(cogBinary) {
			// Resolve relative paths from repo root, not cwd.
			// This handles the case where tests run from integration-tests/
			// but COG_BINARY is set relative to repo root (e.g., "./cog").
			repoRoot, err := findRepoRoot()
			if err != nil {
				return "", err
			}
			cogBinary = filepath.Join(repoRoot, cogBinary)
		}
		return cogBinary, nil
	}

	// Build from source
	return buildCogBinary()
}

// buildCogBinary builds the cog binary from source.
// It finds the repository root, builds wheels if needed, and compiles the binary.
// If the binary already exists, it returns the cached path.
func buildCogBinary() (string, error) {
	// Find repository root (where go.mod with module github.com/replicate/cog exists)
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", fmt.Errorf("failed to find cog repository root: %w", err)
	}

	// Check if binary already exists
	binPath := filepath.Join(repoRoot, "integration-tests", ".bin", "cog")
	if _, err := os.Stat(binPath); err == nil {
		fmt.Printf("Using cached cog binary: %s\n", binPath)
		return binPath, nil
	}

	// Check if wheels exist, build if not
	wheelsDir := filepath.Join(repoRoot, "pkg", "wheels")
	cogWheelExists, _ := filepath.Glob(filepath.Join(wheelsDir, "cog-*.whl"))
	cogletWheelExists, _ := filepath.Glob(filepath.Join(wheelsDir, "coglet-*.whl"))

	if len(cogWheelExists) == 0 || len(cogletWheelExists) == 0 {
		fmt.Println("Building Python wheels...")
		if err := runCommand(repoRoot, "make", "wheel"); err != nil {
			return "", fmt.Errorf("failed to build wheels: %w", err)
		}

		fmt.Println("Generating wheel embeds...")
		if err := runCommand(repoRoot, "go", "generate", "./pkg/wheels"); err != nil {
			return "", fmt.Errorf("failed to generate wheel embeds: %w", err)
		}
	}

	// Build the cog binary
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	fmt.Println("Building cog binary...")
	if err := runCommand(repoRoot, "go", "build", "-o", binPath, "./cmd/cog"); err != nil {
		return "", fmt.Errorf("failed to build cog: %w", err)
	}

	return binPath, nil
}

// findRepoRoot finds the cog repository root by looking for go.mod with the main module
func findRepoRoot() (string, error) {
	// Start from current working directory
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		goMod := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			// Verify it's the main cog repo (not a submodule like integration-tests)
			content, err := os.ReadFile(goMod)
			if err == nil && strings.Contains(string(content), "module github.com/replicate/cog\n") {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find cog repository root")
}

// runCommand runs a command in the specified directory
func runCommand(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Commands returns the custom testscript commands provided by this harness.
func (h *Harness) Commands() map[string]func(ts *testscript.TestScript, neg bool, args []string) {
	return map[string]func(ts *testscript.TestScript, neg bool, args []string){
		"cog": h.cmdCog,
	}
}

// cmdCog implements the 'cog' command for testscript.
func (h *Harness) cmdCog(ts *testscript.TestScript, neg bool, args []string) {
	// Expand environment variables in arguments
	expandedArgs := make([]string, len(args))
	for i, arg := range args {
		expandedArgs[i] = os.Expand(arg, ts.Getenv)
	}

	err := ts.Exec(h.CogBinary, expandedArgs...)
	if neg {
		if err == nil {
			ts.Fatalf("cog command succeeded unexpectedly")
		}
	} else {
		if err != nil {
			ts.Fatalf("cog command failed: %v", err)
		}
	}
}

// Setup returns a testscript Setup function that configures the test environment.
// Fixtures are embedded in the txtar files themselves, so no file copying is needed.
func (h *Harness) Setup(env *testscript.Env) error {
	// Restore real HOME for Docker credential helpers.
	// Docker credential helpers (e.g., docker-credential-desktop) need the real HOME
	// to access the macOS keychain.
	env.Setenv("HOME", h.realHome)

	// Disable update checks during tests
	env.Setenv("COG_NO_UPDATE_CHECK", "1")

	// Generate unique image name for this test run
	imageName := generateUniqueImageName()
	env.Setenv("TEST_IMAGE", imageName)

	// Register cleanup to remove the Docker image after the test
	env.Defer(func() {
		removeDockerImage(imageName)
	})

	return nil
}

// generateUniqueImageName creates a unique Docker image name for test isolation.
func generateUniqueImageName() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a less random but still unique name
		return fmt.Sprintf("cog-test-%d", os.Getpid())
	}
	return fmt.Sprintf("cog-test-%s", hex.EncodeToString(b))
}

// removeDockerImage attempts to remove a Docker image by name.
// It silently ignores errors (image may not exist if test failed early).
func removeDockerImage(imageName string) {
	// Remove all images that match the prefix (base and final images)
	cmd := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", "--filter", fmt.Sprintf("reference=%s*", imageName))
	output, err := cmd.Output()
	if err != nil {
		return
	}

	images := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, img := range images {
		if img == "" {
			continue
		}
		exec.Command("docker", "rmi", "-f", img).Run() //nolint:errcheck
	}
}
