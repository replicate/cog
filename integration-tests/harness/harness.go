// Package harness provides utilities for running cog integration tests.
package harness

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

// Harness provides utilities for running cog integration tests.
type Harness struct {
	CogBinary string
	// realHome is captured at creation time before testscript overrides HOME
	realHome string
	// serverProcs tracks background cog serve processes for cleanup
	serverProcs map[*testscript.TestScript]*exec.Cmd
}

// New creates a new Harness, resolving the cog binary location.
func New() (*Harness, error) {
	cogBinary, err := ResolveCogBinary()
	if err != nil {
		return nil, err
	}
	return &Harness{
		CogBinary:   cogBinary,
		realHome:    os.Getenv("HOME"),
		serverProcs: make(map[*testscript.TestScript]*exec.Cmd),
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
		"cog":        h.cmdCog,
		"curl":       h.cmdCurl,
		"wait-for":   h.cmdWaitFor,
		"retry-curl": h.cmdRetryCurl,
	}
}

// cmdCog implements the 'cog' command for testscript.
// It handles all cog subcommands, with special handling for certain commands.
func (h *Harness) cmdCog(ts *testscript.TestScript, neg bool, args []string) {
	// Check for subcommands that need special handling
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			// Special handling for 'cog serve' - run in background
			h.cmdCogServe(ts, neg, args[1:])
			return
			// Add more special subcommands here as needed:
			// case "run":
			//     h.cmdCogRun(ts, neg, args[1:])
			//     return
		}
	}

	// Default: run cog command normally
	// Note: BUILDKIT_PROGRESS=quiet is set in Setup() to suppress Docker build output
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

	// Use quiet Docker build progress to suppress build output noise in test logs
	// This hides the step-by-step build progress, only showing errors
	env.Setenv("BUILDKIT_PROGRESS", "quiet")

	// Generate unique image name for this test run
	imageName := generateUniqueImageName()
	env.Setenv("TEST_IMAGE", imageName)

	// Register cleanup to remove the Docker image and stop any servers after the test
	env.Defer(func() {
		// Stop any running servers
		for ts := range h.serverProcs {
			h.StopServer(ts)
		}
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

// cmdCogServe implements background 'cog serve' for testscript.
// It starts a cog serve process in the background and waits for it to be healthy.
// Usage: cog serve [flags]
// Exports $SERVER_URL environment variable with the server address.
func (h *Harness) cmdCogServe(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("serve command does not support negation")
	}

	// Check if server is already running
	if _, exists := h.serverProcs[ts]; exists {
		ts.Fatalf("server already running")
	}

	// Allocate a random available port
	port, err := allocatePort()
	if err != nil {
		ts.Fatalf("failed to allocate port: %v", err)
	}

	// Build command arguments
	cmdArgs := []string{"serve", "-p", strconv.Itoa(port)}
	cmdArgs = append(cmdArgs, args...)

	// Expand environment variables in arguments
	expandedArgs := make([]string, len(cmdArgs))
	for i, arg := range cmdArgs {
		expandedArgs[i] = os.Expand(arg, ts.Getenv)
	}

	// Start the server process
	cmd := exec.Command(h.CogBinary, expandedArgs...)
	cmd.Dir = ts.Getenv("WORK")

	// Build environment from testscript
	var env []string
	for _, key := range []string{"HOME", "PATH", "COG_NO_UPDATE_CHECK", "BUILDKIT_PROGRESS", "TEST_IMAGE"} {
		if val := ts.Getenv(key); val != "" {
			env = append(env, fmt.Sprintf("%s=%s", key, val))
		}
	}
	cmd.Env = env

	// Capture server output for debugging
	cmd.Stdout = ts.Stdout()
	cmd.Stderr = ts.Stderr()

	if err := cmd.Start(); err != nil {
		ts.Fatalf("failed to start server: %v", err)
	}

	// Store the process for cleanup
	h.serverProcs[ts] = cmd

	// Wait for server to be healthy
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	ts.Setenv("SERVER_URL", serverURL)

	if !waitForServer(serverURL, 60*time.Second) {
		// Try to get server output for debugging
		cmd.Process.Kill()
		ts.Fatalf("server did not become healthy within timeout")
	}
}

// cmdCurl implements the 'curl' command for testscript.
// It makes HTTP requests to the server started with 'serve'.
// Usage: curl [method] [path] [body]
// Examples:
//
//	curl GET /health-check
//	curl POST /predictions '{"input":{"s":"hello"}}'
func (h *Harness) cmdCurl(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("curl: usage: curl [method] [path] [body]")
	}

	serverURL := ts.Getenv("SERVER_URL")
	if serverURL == "" {
		ts.Fatalf("curl: SERVER_URL not set (did you call 'serve' first?)")
	}

	method := args[0]
	path := args[1]
	var body string
	if len(args) > 2 {
		body = args[2]
	}

	// Make the HTTP request
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(method, serverURL+path, strings.NewReader(body))
	if err != nil {
		ts.Fatalf("curl: failed to create request: %v", err)
	}

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		if neg {
			// Expected to fail
			return
		}
		ts.Fatalf("curl: request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	var respBodyBuilder strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			respBodyBuilder.Write(buf[:n])
		}
		if err != nil {
			if err.Error() != "EOF" {
				ts.Fatalf("curl: failed to read response: %v", err)
			}
			break
		}
	}
	respBody := respBodyBuilder.String()

	// Check status code expectations
	statusOK := resp.StatusCode >= 200 && resp.StatusCode < 300
	if neg {
		if statusOK {
			ts.Fatalf("curl: expected failure but got status %d", resp.StatusCode)
		}
	} else {
		if !statusOK {
			// For error responses, try to show detailed error from JSON
			errorMsg := respBody
			if len(errorMsg) > 500 {
				errorMsg = errorMsg[:500] + "..."
			}
			ts.Logf("curl: full response body: %s", respBody)
			ts.Fatalf("curl: request failed with status %d: %s", resp.StatusCode, errorMsg)
		}
	}

	// Write response body to stdout for assertions
	ts.Stdout().Write([]byte(respBody))
}

// StopServer stops the background server process for a test script.
func (h *Harness) StopServer(ts *testscript.TestScript) {
	if cmd, exists := h.serverProcs[ts]; exists {
		// Try graceful shutdown first
		serverURL := ts.Getenv("SERVER_URL")
		if serverURL != "" {
			client := &http.Client{Timeout: 5 * time.Second}
			client.Post(serverURL+"/shutdown", "application/json", nil) //nolint:errcheck
			time.Sleep(100 * time.Millisecond)
		}

		// Force kill if still running
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
		delete(h.serverProcs, ts)
	}
}

// allocatePort finds an available TCP port.
func allocatePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// healthCheckResponse represents the JSON response from /health-check
type healthCheckResponse struct {
	Status string `json:"status"`
}

// waitForServer polls the server's health-check endpoint until it returns READY status.
// The server may return HTTP 200 while still in STARTING state (during setup),
// so we must check the actual status field in the response.
func waitForServer(serverURL string, timeout time.Duration) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(serverURL + "/health-check")
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}

			var health healthCheckResponse
			if err := json.Unmarshal(body, &health); err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}

			// Only return success when the server is actually READY
			// (setup has completed successfully)
			if health.Status == "READY" {
				return true
			}

			// If setup failed, no point waiting
			if health.Status == "SETUP_FAILED" || health.Status == "DEFUNCT" {
				return false
			}
		} else {
			resp.Body.Close()
		}

		time.Sleep(200 * time.Millisecond)
	}

	return false
}

// cmdWaitFor implements the 'wait-for' command for testscript.
// It waits for a specific condition to become true with retries.
// Usage:
//
//	wait-for file <path> [timeout]           - Wait for file to exist
//	wait-for http <url> [status] [timeout]   - Wait for HTTP endpoint
//	wait-for not-empty <file> [timeout]      - Wait for file with content
func (h *Harness) cmdWaitFor(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("wait-for: usage: wait-for [file|http|not-empty] <arg> [timeout]")
	}

	condition := args[0]
	target := args[1]

	// Default timeout of 30 seconds, can be overridden
	timeout := 30 * time.Second
	if len(args) > 2 {
		if duration, err := time.ParseDuration(args[len(args)-1]); err == nil {
			timeout = duration
		}
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		var conditionMet bool

		switch condition {
		case "file":
			// Wait for file to exist
			targetPath := filepath.Join(ts.Getenv("WORK"), target)
			_, err := os.Stat(targetPath)
			conditionMet = err == nil

		case "not-empty":
			// Wait for file to exist with non-empty content
			targetPath := filepath.Join(ts.Getenv("WORK"), target)
			data, err := os.ReadFile(targetPath)
			conditionMet = err == nil && len(data) > 0

		case "http":
			// Wait for HTTP endpoint to return expected status
			expectedStatus := 200
			if len(args) > 2 {
				if status, err := strconv.Atoi(args[2]); err == nil {
					expectedStatus = status
				}
			}

			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get(target)
			if err == nil {
				conditionMet = resp.StatusCode == expectedStatus
				resp.Body.Close()
			}

		default:
			ts.Fatalf("wait-for: unknown condition: %s", condition)
		}

		if neg {
			// For negation, we want the condition to remain false
			if !conditionMet {
				return
			}
		} else {
			// Normal case: condition should become true
			if conditionMet {
				return
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	if neg {
		ts.Fatalf("wait-for: condition became true (expected to remain false)")
	} else {
		ts.Fatalf("wait-for: timeout waiting for condition: %s %s", condition, target)
	}
}

// cmdRetryCurl implements the 'retry-curl' command for testscript.
// It's like curl but retries on failure, useful for waiting for subprocess initialization.
// Usage: retry-curl [method] [path] [body] [max-attempts] [retry-delay]
// Examples:
//
//	retry-curl POST /predictions '{"input":{"s":"hello"}}' 10 1s
func (h *Harness) cmdRetryCurl(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("retry-curl: usage: retry-curl [method] [path] [body] [max-attempts] [retry-delay]")
	}

	serverURL := ts.Getenv("SERVER_URL")
	if serverURL == "" {
		ts.Fatalf("retry-curl: SERVER_URL not set (did you call 'cog serve' first?)")
	}

	method := args[0]
	path := args[1]
	var body string
	if len(args) > 2 && args[2] != "" && args[2][0] == '{' {
		body = args[2]
	}

	// Parse max attempts (default: 10)
	maxAttempts := 10
	if len(args) > 3 {
		if attempts, err := strconv.Atoi(args[3]); err == nil {
			maxAttempts = attempts
		}
	}

	// Parse retry delay (default: 1s)
	retryDelay := 1 * time.Second
	if len(args) > 4 {
		if duration, err := time.ParseDuration(args[4]); err == nil {
			retryDelay = duration
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}

	var lastErr error
	var lastStatus int
	var lastBody string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequest(method, serverURL+path, strings.NewReader(body))
		if err != nil {
			lastErr = err
			time.Sleep(retryDelay)
			continue
		}

		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(retryDelay)
			continue
		}

		// Read response body
		var respBodyBuilder strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				respBodyBuilder.Write(buf[:n])
			}
			if readErr != nil {
				if readErr.Error() != "EOF" {
					ts.Fatalf("retry-curl: failed to read response: %v", readErr)
				}
				break
			}
		}
		respBody := respBodyBuilder.String()
		resp.Body.Close()

		lastStatus = resp.StatusCode
		lastBody = respBody

		// Check if this is a successful response
		statusOK := resp.StatusCode >= 200 && resp.StatusCode < 300

		if neg {
			if !statusOK {
				// Expected to fail - success!
				return
			}
		} else {
			if statusOK {
				// Success - write body to stdout
				ts.Stdout().Write([]byte(respBody))
				return
			}
		}

		// If this isn't the last attempt, wait before retrying
		if attempt < maxAttempts {
			time.Sleep(retryDelay)
		}
	}

	// All attempts failed
	if neg {
		ts.Fatalf("retry-curl: expected failure but got status %d after %d attempts", lastStatus, maxAttempts)
	} else {
		if lastErr != nil {
			ts.Fatalf("retry-curl: all %d attempts failed with error: %v", maxAttempts, lastErr)
		} else {
			errorMsg := lastBody
			if len(errorMsg) > 500 {
				errorMsg = errorMsg[:500] + "..."
			}
			ts.Logf("retry-curl: full response body: %s", lastBody)
			ts.Fatalf("retry-curl: all %d attempts failed with status %d: %s", maxAttempts, lastStatus, errorMsg)
		}
	}
}
