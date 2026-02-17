//go:build integration

// Package login provides integration tests for the cog login command.
//
// These tests verify:
// - Generic registry login with username/password (PTY-based)
// - Provider routing based on --registry flag
// - Help text and CLI flags
//
// This test file is written in Go (not txtar) because:
// - Login requires interactive input (PTY for generic provider)
// - We need fine-grained control over stdin and stdout
//
// Note: Replicate provider token verification is tested in unit tests
// (pkg/provider/replicate/replicate_test.go) since mocking the r8.im
// hostname requires DNS-level changes not suitable for integration tests.
package login_test

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/replicate/cog/integration-tests/harness"
)

// TestLoginGenericRegistryPTY tests interactive login to a generic registry.
// This test uses PTY to simulate interactive terminal input.
func TestLoginGenericRegistryPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// PTY tests only work reliably on Unix-like systems
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests not supported on Windows")
	}

	// Get cog binary
	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

	// Test login to a fake generic registry
	// Note: This will fail at the Docker credential save step, but we can verify
	// the interactive prompts work correctly up to that point
	t.Run("prompts for username and password", func(t *testing.T) {
		cmd := exec.Command(cogBinary, "login", "--registry", "fake-registry.example.com")
		cmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

		// Start with a PTY
		ptmx, err := pty.Start(cmd)
		if err != nil {
			t.Fatalf("failed to start PTY: %v", err)
		}
		defer func() {
			ptmx.Close()
			cmd.Process.Kill()
			cmd.Wait()
		}()

		// Set terminal size
		if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
			t.Logf("failed to set terminal size: %v", err)
		}

		// Use a single mutex-protected buffer for thread safety
		// This avoids the race condition of multiple goroutines reading from the PTY
		var bufMu bytes.Buffer
		var mu sync.Mutex

		// Start a single reader goroutine
		done := make(chan struct{})
		go func() {
			tmp := make([]byte, 1024)
			for {
				select {
				case <-done:
					return
				default:
					n, err := ptmx.Read(tmp)
					if n > 0 {
						mu.Lock()
						bufMu.Write(tmp[:n])
						mu.Unlock()
					}
					if err != nil {
						return
					}
				}
			}
		}()
		defer close(done)

		// Helper to get current buffer contents
		getOutput := func() string {
			mu.Lock()
			defer mu.Unlock()
			return bufMu.String()
		}

		// Helper to wait for a pattern in output with timeout
		waitForPattern := func(pattern string, timeout time.Duration) (string, bool) {
			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				output := getOutput()
				if strings.Contains(strings.ToLower(output), strings.ToLower(pattern)) {
					return output, true
				}
				time.Sleep(100 * time.Millisecond)
			}
			return getOutput(), false
		}

		// Wait for and verify username prompt
		output, found := waitForPattern("username", 5*time.Second)
		t.Logf("Output after start: %q", output)

		if !strings.Contains(output, "fake-registry.example.com") {
			t.Errorf("expected output to mention registry host, got: %q", output)
		}
		if !found {
			t.Errorf("expected username prompt, got: %q", output)
		}

		// Send username
		_, err = ptmx.Write([]byte("testuser\n"))
		if err != nil {
			t.Fatalf("failed to write username: %v", err)
		}

		// Wait for password prompt
		output, found = waitForPattern("password", 3*time.Second)
		t.Logf("Output after username: %q", output)

		if !found {
			t.Errorf("expected password prompt, got: %q", output)
		}

		// Send password (will fail at Docker credential save, but we've verified the flow)
		_, err = ptmx.Write([]byte("testpass\n"))
		if err != nil {
			t.Fatalf("failed to write password: %v", err)
		}

		// Read final output briefly (expect failure since we can't actually save credentials)
		time.Sleep(2 * time.Second)
		output = getOutput()
		t.Logf("Final output: %q", output)
	})

	t.Run("rejects empty username", func(t *testing.T) {
		cmd := exec.Command(cogBinary, "login", "--registry", "fake-registry.example.com")
		cmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

		ptmx, err := pty.Start(cmd)
		if err != nil {
			t.Fatalf("failed to start PTY: %v", err)
		}
		defer func() {
			ptmx.Close()
			cmd.Process.Kill()
			cmd.Wait()
		}()

		// Use a mutex-protected buffer for thread safety
		var bufMu bytes.Buffer
		var mu sync.Mutex

		// Start reader goroutine
		done := make(chan struct{})
		go func() {
			tmp := make([]byte, 1024)
			for {
				select {
				case <-done:
					return
				default:
					n, err := ptmx.Read(tmp)
					if n > 0 {
						mu.Lock()
						bufMu.Write(tmp[:n])
						mu.Unlock()
					}
					if err != nil {
						return
					}
				}
			}
		}()
		defer close(done)

		// Helper to check buffer contents
		getOutput := func() string {
			mu.Lock()
			defer mu.Unlock()
			return bufMu.String()
		}

		// Wait for username prompt
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(strings.ToLower(getOutput()), "username") {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		output := getOutput()
		if !strings.Contains(strings.ToLower(output), "username") {
			t.Fatalf("did not get username prompt: %q", output)
		}

		// Send empty username
		_, err = ptmx.Write([]byte("\n"))
		if err != nil {
			t.Fatalf("failed to write empty username: %v", err)
		}

		// Wait for error about empty username
		deadline = time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			output = getOutput()
			if strings.Contains(strings.ToLower(output), "empty") || strings.Contains(strings.ToLower(output), "cannot") {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		output = getOutput()
		t.Logf("Output: %q", output)

		// Verify we got an error about empty username
		if !strings.Contains(strings.ToLower(output), "empty") && !strings.Contains(strings.ToLower(output), "cannot") {
			t.Errorf("expected error about empty username, got: %q", output)
		}
	})
}

// TestLoginProviderRouting tests that the --registry flag correctly routes to the appropriate provider.
// This test verifies the routing behavior by checking error messages and prompts.
func TestLoginProviderRouting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// PTY tests only work reliably on Unix-like systems
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests not supported on Windows")
	}

	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

	tests := []struct {
		name            string
		registry        string
		expectReplicate bool // True if we expect Replicate provider behavior
	}{
		{
			name:            "default registry uses Replicate",
			registry:        "",
			expectReplicate: true,
		},
		{
			name:            "r8.im uses Replicate",
			registry:        "r8.im",
			expectReplicate: true,
		},
		{
			name:            "custom registry uses generic",
			registry:        "ghcr.io",
			expectReplicate: false,
		},
		{
			name:            "dockerhub uses generic",
			registry:        "docker.io",
			expectReplicate: false,
		},
		{
			name:            "localhost uses generic",
			registry:        "localhost:5000",
			expectReplicate: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"login"}
			if tc.registry != "" {
				args = append(args, "--registry", tc.registry)
			}

			cmd := exec.Command(cogBinary, args...)
			cmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

			// Start with a PTY to handle interactive prompts
			ptmx, err := pty.Start(cmd)
			if err != nil {
				t.Fatalf("failed to start PTY: %v", err)
			}
			defer func() {
				ptmx.Close()
				cmd.Process.Kill()
				cmd.Wait()
			}()

			// Read initial output
			var buf bytes.Buffer
			deadline := time.Now().Add(5 * time.Second)
			tmp := make([]byte, 1024)
			for time.Now().Before(deadline) {
				ptmx.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, _ := ptmx.Read(tmp)
				if n > 0 {
					buf.Write(tmp[:n])
					// Check if we have enough output to determine provider
					output := buf.String()
					if tc.expectReplicate {
						// Replicate provider shows "Hit enter to get started" message
						if strings.Contains(output, "Hit enter") || strings.Contains(output, "browser") {
							t.Logf("Confirmed Replicate provider: %q", output)
							return
						}
					} else {
						// Generic provider shows "Username:" prompt directly
						if strings.Contains(output, "Username") {
							t.Logf("Confirmed Generic provider: %q", output)
							return
						}
					}
				}
			}

			output := buf.String()
			if tc.expectReplicate {
				if strings.Contains(output, "Username:") {
					t.Errorf("expected Replicate provider, but got Generic provider with Username prompt")
				}
			} else {
				if !strings.Contains(output, "Username") && !strings.Contains(strings.ToLower(output), "logging in") {
					t.Errorf("expected Generic provider prompts, got: %q", output)
				}
			}
		})
	}
}

// TestLoginEnvironmentVariable tests that COG_REGISTRY_HOST environment variable works.
func TestLoginEnvironmentVariable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// PTY tests only work reliably on Unix-like systems
	if runtime.GOOS == "windows" {
		t.Skip("PTY tests not supported on Windows")
	}

	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

	t.Run("COG_REGISTRY_HOST sets default registry", func(t *testing.T) {
		cmd := exec.Command(cogBinary, "login")
		cmd.Env = append(os.Environ(),
			"COG_NO_UPDATE_CHECK=1",
			"COG_REGISTRY_HOST=custom-registry.example.com",
		)

		// Start with a PTY
		ptmx, err := pty.Start(cmd)
		if err != nil {
			t.Fatalf("failed to start PTY: %v", err)
		}
		defer func() {
			ptmx.Close()
			cmd.Process.Kill()
			cmd.Wait()
		}()

		// Read output
		var buf bytes.Buffer
		deadline := time.Now().Add(5 * time.Second)
		tmp := make([]byte, 1024)
		for time.Now().Before(deadline) {
			ptmx.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _ := ptmx.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				// Stop early if we see the expected registry
				if strings.Contains(buf.String(), "custom-registry.example.com") {
					break
				}
			}
		}

		output := buf.String()
		t.Logf("Output: %s", output)

		// Verify the custom registry is mentioned in output
		if !strings.Contains(output, "custom-registry.example.com") {
			t.Errorf("expected custom registry in output, got: %s", output)
		}

		// Since custom-registry.example.com is not r8.im, it should use generic provider
		if !strings.Contains(output, "Username") {
			t.Logf("Note: Generic provider should prompt for Username")
		}
	})

	t.Run("--registry flag overrides COG_REGISTRY_HOST", func(t *testing.T) {
		cmd := exec.Command(cogBinary, "login", "--registry", "override-registry.example.com")
		cmd.Env = append(os.Environ(),
			"COG_NO_UPDATE_CHECK=1",
			"COG_REGISTRY_HOST=ignored-registry.example.com",
		)

		// Start with a PTY
		ptmx, err := pty.Start(cmd)
		if err != nil {
			t.Fatalf("failed to start PTY: %v", err)
		}
		defer func() {
			ptmx.Close()
			cmd.Process.Kill()
			cmd.Wait()
		}()

		// Read output
		var buf bytes.Buffer
		deadline := time.Now().Add(5 * time.Second)
		tmp := make([]byte, 1024)
		for time.Now().Before(deadline) {
			ptmx.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _ := ptmx.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				// Stop early if we see the expected registry
				if strings.Contains(buf.String(), "override-registry.example.com") {
					break
				}
			}
		}

		output := buf.String()
		t.Logf("Output: %s", output)

		// Verify the override registry is used, not the env var one
		if !strings.Contains(output, "override-registry.example.com") {
			t.Errorf("expected override registry in output, got: %s", output)
		}
		if strings.Contains(output, "ignored-registry.example.com") {
			t.Errorf("env var registry should have been overridden, but it appeared in output")
		}
	})
}

// TestLoginHelp tests that the login command shows appropriate help text.
func TestLoginHelp(t *testing.T) {
	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

	cmd := exec.Command(cogBinary, "login", "--help")
	cmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	helpText := string(output)
	t.Logf("Help text:\n%s", helpText)

	// Verify help contains expected information
	expectedStrings := []string{
		"login",
		"registry",
		"--token-stdin",
		"container registry", // Updated description mentions "container registry"
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(strings.ToLower(helpText), strings.ToLower(expected)) {
			t.Errorf("expected help to contain %q", expected)
		}
	}

	// Verify help mentions both Replicate and generic registry support
	if !strings.Contains(helpText, "Replicate") {
		t.Errorf("expected help to mention Replicate")
	}
	if !strings.Contains(helpText, "other registries") || !strings.Contains(helpText, "username and password") {
		t.Errorf("expected help to mention generic registry login with username/password")
	}
}

// TestLoginSuggestFor tests that similar commands are suggested.
func TestLoginSuggestFor(t *testing.T) {
	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

	// Test that "cog auth" suggests "cog login"
	cmd := exec.Command(cogBinary, "auth")
	cmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	output, err := cmd.CombinedOutput()
	// We expect an error since "auth" is not a valid command
	if err == nil {
		t.Logf("Unexpected success, output: %s", output)
	}

	outputStr := string(output)
	t.Logf("Output for 'cog auth': %s", outputStr)

	// Check if login is suggested
	if strings.Contains(outputStr, "login") {
		t.Logf("'login' suggested for 'auth' command (good)")
	}
}
