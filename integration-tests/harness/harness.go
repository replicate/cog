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
// 2. "cog" in PATH
func ResolveCogBinary() (string, error) {
	if cogBinary := os.Getenv("COG_BINARY"); cogBinary != "" {
		if !filepath.IsAbs(cogBinary) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			cogBinary = filepath.Join(cwd, cogBinary)
		}
		return cogBinary, nil
	}

	// Fall back to cog in PATH
	cogPath, err := exec.LookPath("cog")
	if err != nil {
		return "", err
	}
	return cogPath, nil
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
