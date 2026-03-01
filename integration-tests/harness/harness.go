// Package harness provides utilities for running cog integration tests.
package harness

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/registry_testhelpers"
)

// propagatedEnvVars lists host environment variables that should be propagated
// into testscript environments (Setup) and background processes (cmdCogServe).
// Keep this list in sync: if you add a new env var to propagate, add it here.
var propagatedEnvVars = []string{
	"COG_SDK_WHEEL",     // SDK wheel override
	"COGLET_WHEEL",      // coglet wheel override
	"RUST_LOG",          // Rust logging control
	"COG_CA_CERT",       // custom CA certificates (e.g. Cloudflare WARP)
	"BUILDKIT_PROGRESS", // Docker build output format
}

// Harness provides utilities for running cog integration tests.
// serverInfo tracks a running cog serve process and its port
type serverInfo struct {
	cmd  *exec.Cmd
	port int
}

// registryInfo tracks a running test registry container
type registryInfo struct {
	container *registry_testhelpers.RegistryContainer
	cleanup   func()
	host      string // e.g., "localhost:5432"
}

// mockUploadRecord records a single upload received by the mock upload server.
type mockUploadRecord struct {
	Path        string
	ContentType string
	Size        int
}

// mockUploadServer is a lightweight HTTP server that accepts PUT requests
// and records what was uploaded.
type mockUploadServer struct {
	server  *http.Server
	port    int
	mu      sync.Mutex
	uploads []mockUploadRecord
}

// webhookResult is the summary written to stdout by webhook-server-wait.
type webhookResult struct {
	Status     string          `json:"status"`
	OutputSize int             `json:"output_size"`
	HasError   bool            `json:"has_error"`
	Metrics    json.RawMessage `json:"metrics,omitempty"`
}

// webhookServer accepts prediction webhook callbacks from coglet.
// It parses the JSON payload to extract status and output size, without
// ever exposing the (potentially huge) output to testscript's log buffer.
type webhookServer struct {
	server *http.Server
	port   int
	mu     sync.Mutex
	result *webhookResult
	done   chan struct{} // closed on first terminal webhook
}

type Harness struct {
	CogBinary string
	// realHome is captured at creation time before testscript overrides HOME
	realHome string
	// repoRoot is the path to the cog repository root
	repoRoot string
	// serverProcs tracks background cog serve processes for cleanup, keyed by work directory
	serverProcs   map[string]*serverInfo
	serverProcsMu sync.Mutex
	// registries tracks test registry containers for cleanup, keyed by work directory
	registries   map[string]*registryInfo
	registriesMu sync.Mutex
	// uploadServers tracks mock upload servers for cleanup, keyed by work directory
	uploadServers   map[string]*mockUploadServer
	uploadServersMu sync.Mutex
	// webhookServers tracks webhook receiver servers for cleanup, keyed by work directory
	webhookServers   map[string]*webhookServer
	webhookServersMu sync.Mutex
}

// New creates a new Harness, resolving the cog binary location.
func New() (*Harness, error) {
	cogBinary, err := ResolveCogBinary()
	if err != nil {
		return nil, err
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return nil, err
	}
	return &Harness{
		CogBinary:      cogBinary,
		realHome:       os.Getenv("HOME"),
		repoRoot:       repoRoot,
		serverProcs:    make(map[string]*serverInfo),
		registries:     make(map[string]*registryInfo),
		uploadServers:  make(map[string]*mockUploadServer),
		webhookServers: make(map[string]*webhookServer),
	}, nil
}

// ResolveCogBinary finds the cog binary to use for tests.
// It checks (in order):
// 1. COG_BINARY environment variable
// 2. Build from source (if in cog repository)
func ResolveCogBinary() (string, error) {
	if cogBinary := os.Getenv("COG_BINARY"); cogBinary != "" {
		if !filepath.IsAbs(cogBinary) {
			// Resolve relative paths from repo root, not the test package directory.
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
	var (
		wheelsDir            = filepath.Join(repoRoot, "pkg", "wheels")
		cogWheelExists, _    = filepath.Glob(filepath.Join(wheelsDir, "cog-*.whl"))
		cogletWheelExists, _ = filepath.Glob(filepath.Join(wheelsDir, "coglet-*.whl"))
	)

	if len(cogWheelExists) == 0 || len(cogletWheelExists) == 0 {
		fmt.Println("Building Python wheels...")
		if err := runCommand(repoRoot, "mise", "run", "build:wheels"); err != nil {
			return "", fmt.Errorf("failed to build wheels: %w", err)
		}

		fmt.Println("Generating wheel embeds...")
		if err := runCommand(repoRoot, "go", "generate", "./pkg/wheels"); err != nil {
			return "", fmt.Errorf("failed to generate wheel embeds: %w", err)
		}
	}

	// Build the cog binary
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
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
			// Verify it's the cog repo root (matches the expected module path)
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
	// Register all commands
	commands := []Command{
		// Built-in commands (defined in this file)
		NewCommand("cog", h.cmdCog),
		NewCommand("curl", h.cmdCurl),
		NewCommand("wait-for", h.cmdWaitFor),
		NewCommand("docker-run", h.cmdDockerRun),

		// Registry and OCI bundle testing commands
		NewCommand("registry-start", h.cmdRegistryStart),
		NewCommand("registry-inspect", h.cmdRegistryInspect),
		NewCommand("docker-push", h.cmdDockerPush),
		NewCommand("mock-weights", h.cmdMockWeights),

		// Mock upload server commands
		NewCommand("upload-server-start", h.cmdUploadServerStart),
		NewCommand("upload-server-count", h.cmdUploadServerCount),

		// Webhook receiver commands
		NewCommand("webhook-server-start", h.cmdWebhookServerStart),
		NewCommand("webhook-server-wait", h.cmdWebhookServerWait),

		// PTY command (defined in cmd_pty.go)
		&PtyRunCommand{harness: h},
	}

	// Build the command map
	result := make(map[string]func(ts *testscript.TestScript, neg bool, args []string))
	for _, cmd := range commands {
		result[cmd.Name()] = cmd.Run
	}
	return result
}

// cmdCog implements the 'cog' command for testscript.
// It handles all cog subcommands, with special handling for certain commands.
func (h *Harness) cmdCog(ts *testscript.TestScript, neg bool, args []string) {
	// Check for subcommands that need special handling
	if len(args) > 0 && args[0] == "serve" {
		// Special handling for 'cog serve' - run in background
		h.cmdCogServe(ts, neg, args[1:])
		return
	}

	// Default: run cog command normally
	expandedArgs := make([]string, len(args))
	for i, arg := range args {
		expandedArgs[i] = os.Expand(arg, ts.Getenv)
	}

	err := ts.Exec(h.CogBinary, expandedArgs...)
	if neg {
		if err == nil {
			ts.Fatalf("cog command succeeded unexpectedly")
		}
		return
	}

	if err != nil {
		ts.Fatalf("cog command failed: %v", err)
	}
}

// Setup returns a testscript Setup function that configures the test environment.
// Fixtures are embedded in the txtar files themselves, so no file copying is needed.
func (h *Harness) Setup(env *testscript.Env) error {
	// Restore real HOME for Docker credential helpers.
	// Docker credential helpers (e.g., docker-credential-desktop) need the real HOME
	// to access the macOS keychain.
	env.Setenv("HOME", h.realHome)

	// Export repo root for tests that need to reference files outside the work directory
	env.Setenv("REPO_ROOT", h.repoRoot)

	// Disable update checks during tests
	env.Setenv("COG_NO_UPDATE_CHECK", "1")

	// Propagate host env vars listed in propagatedEnvVars
	for _, key := range propagatedEnvVars {
		if val := os.Getenv(key); val != "" {
			env.Setenv(key, val)
		}
	}

	// Generate unique image name for this test run
	imageName := generateUniqueImageName()
	env.Setenv("TEST_IMAGE", imageName)

	// Capture the work directory for this test (used as key for server tracking)
	workDir := env.WorkDir

	// Register cleanup to remove the Docker image, stop servers, and cleanup registries
	env.Defer(func() {
		// Stop the server for this specific test (if any)
		h.stopServerByWorkDir(workDir)
		// Stop the registry for this specific test (if any)
		h.stopRegistryByWorkDir(workDir)
		// Stop the upload server for this specific test (if any)
		h.stopUploadServerByWorkDir(workDir)
		// Stop the webhook server for this specific test (if any)
		h.stopWebhookServerByWorkDir(workDir)
		removeDockerImage(imageName)
	})

	return nil
}

// generateUniqueImageName creates a unique Docker image name for test isolation.
func generateUniqueImageName() string {
	b := make([]byte, 5)
	if _, err := cryptorand.Read(b); err != nil {
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

	for img := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if img == "" {
			continue
		}
		exec.Command("docker", "rmi", "-f", img).Run() //nolint:errcheck,gosec
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

	workDir := ts.Getenv("WORK")

	// Check if server is already running
	h.serverProcsMu.Lock()
	if _, exists := h.serverProcs[workDir]; exists {
		h.serverProcsMu.Unlock()
		ts.Fatalf("server already running")
	}
	h.serverProcsMu.Unlock()

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
	cmd.Dir = workDir

	// Build environment from testscript.
	// Always include core vars, plus everything from propagatedEnvVars.
	var env []string
	for _, key := range []string{"HOME", "PATH", "REPO_ROOT", "COG_NO_UPDATE_CHECK", "TEST_IMAGE"} {
		if val := ts.Getenv(key); val != "" {
			env = append(env, key+"="+val)
		}
	}
	for _, key := range propagatedEnvVars {
		if val := ts.Getenv(key); val != "" {
			env = append(env, key+"="+val)
		}
	}
	cmd.Env = env

	// Capture server output for debugging
	cmd.Stdout = ts.Stdout()
	cmd.Stderr = ts.Stderr()

	if err := cmd.Start(); err != nil {
		ts.Fatalf("failed to start server: %v", err)
	}

	// Store the process for cleanup (keyed by work directory)
	h.serverProcsMu.Lock()
	h.serverProcs[workDir] = &serverInfo{cmd: cmd, port: port}
	h.serverProcsMu.Unlock()

	// Wait for server to be healthy
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	ts.Setenv("SERVER_URL", serverURL)

	if !waitForServer(serverURL, 60*time.Second) {
		// Try to get server output for debugging
		_ = cmd.Process.Kill()
		ts.Fatalf("server did not become healthy within timeout")
	}
}

// cmdCurl implements the 'curl' command for testscript.
// It makes HTTP requests to the server started with 'serve'.
// Includes built-in retry logic (10 attempts, 500ms delay) for resilience.
// Usage: curl [-H key:value]... [method] [path] [body]
// Examples:
//
//	curl GET /health-check
//	curl POST /predictions '{"input":{"s":"hello"}}'
//	curl -H Prefer:respond-async POST /predictions '{"input":{}}'
func (h *Harness) cmdCurl(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("curl: usage: curl [-H key:value]... [method] [path] [body]")
	}

	// Parse -H flags for extra headers
	var extraHeaders [][2]string
	for len(args) >= 2 && args[0] == "-H" {
		kv := args[1]
		parts := strings.SplitN(kv, ":", 2)
		if len(parts) != 2 {
			ts.Fatalf("curl: invalid header %q (expected key:value)", kv)
		}
		extraHeaders = append(extraHeaders, [2]string{
			strings.TrimSpace(parts[0]),
			strings.TrimSpace(parts[1]),
		})
		args = args[2:]
	}

	if len(args) < 2 {
		ts.Fatalf("curl: usage: curl [-H key:value]... [method] [path] [body]")
	}

	serverURL := ts.Getenv("SERVER_URL")
	if serverURL == "" {
		ts.Fatalf("curl: SERVER_URL not set (did you call 'cog serve' first?)")
	}

	method := args[0]
	path := args[1]
	var body string
	if len(args) > 2 {
		body = os.Expand(args[2], ts.Getenv)
	}

	// Retry settings
	maxAttempts := 10
	retryDelay := 500 * time.Millisecond

	client := &http.Client{Timeout: 10 * time.Second}

	var (
		lastErr    error
		lastStatus int
		lastBody   string
	)

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
		for _, h := range extraHeaders {
			req.Header.Set(h[0], h[1])
		}

		resp, err := client.Do(req) //nolint:gosec // G704: URL from test harness, not user input
		if err != nil {
			lastErr = err
			time.Sleep(retryDelay)
			continue
		}

		// Read response body
		respBodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			ts.Fatalf("curl: failed to read response: %v", readErr)
		}
		respBody := string(respBodyBytes)
		_ = resp.Body.Close()

		lastStatus = resp.StatusCode
		lastBody = respBody
		lastErr = nil

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
				_, _ = ts.Stdout().Write([]byte(respBody))
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
		ts.Fatalf("curl: expected failure but got status %d after %d attempts", lastStatus, maxAttempts)
		return
	}

	if lastErr != nil {
		ts.Fatalf("curl: all %d attempts failed with error: %v", maxAttempts, lastErr)
		return
	}

	errorMsg := lastBody
	if len(errorMsg) > 500 {
		errorMsg = errorMsg[:500] + "..."
	}
	ts.Logf("curl: full response body: %s", lastBody)
	ts.Fatalf("curl: all %d attempts failed with status %d: %s", maxAttempts, lastStatus, errorMsg)
}

// StopServer stops the background server process for a test script.
func (h *Harness) StopServer(ts *testscript.TestScript) {
	workDir := ts.Getenv("WORK")
	h.stopServerByWorkDir(workDir)
}

// stopServerByWorkDir stops the server process associated with a work directory.
func (h *Harness) stopServerByWorkDir(workDir string) {
	h.serverProcsMu.Lock()
	info, exists := h.serverProcs[workDir]
	if !exists {
		h.serverProcsMu.Unlock()
		return
	}
	delete(h.serverProcs, workDir)
	h.serverProcsMu.Unlock()

	// Try graceful shutdown first via /shutdown endpoint
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", info.port)
	shutdownURL := serverURL + "/shutdown"
	resp, err := http.Post(shutdownURL, "application/json", nil) //nolint:gosec,noctx
	if err == nil {
		_ = resp.Body.Close()
	}

	// Force kill the cog process if still running
	if info.cmd.Process != nil {
		_ = info.cmd.Process.Kill()
	}
	_ = info.cmd.Wait()

	// Also kill any Docker container that may still be running on this port
	// Find container by port and kill it
	output, err := exec.Command("docker", "ps", "-q", "--filter", fmt.Sprintf("publish=%d", info.port)).Output()
	if err == nil && len(output) > 0 {
		containerID := strings.TrimSpace(string(output))
		if containerID != "" {
			exec.Command("docker", "kill", containerID).Run() //nolint:errcheck,gosec
		}
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

		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}

			var health healthCheckResponse
			if err := json.Unmarshal(body, &health); err != nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}

			// Return success when the server has completed setup
			// READY = setup completed, healthcheck passed (or no healthcheck)
			// UNHEALTHY = setup completed, but user healthcheck failed
			// BUSY = setup completed, prediction in progress
			if health.Status == "READY" || health.Status == "UNHEALTHY" || health.Status == "BUSY" {
				return true
			}

			// If setup failed, no point waiting
			if health.Status == "SETUP_FAILED" || health.Status == "DEFUNCT" {
				return false
			}
		} else {
			_ = resp.Body.Close()
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

	var (
		condition = args[0]
		target    = args[1]

		// Default timeout of 30 seconds, can be overridden
		timeout = 30 * time.Second
	)

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
			expectedStatus := http.StatusOK
			if len(args) > 2 {
				if status, err := strconv.Atoi(args[2]); err == nil {
					expectedStatus = status
				}
			}

			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get(target)
			if err == nil {
				conditionMet = resp.StatusCode == expectedStatus
				_ = resp.Body.Close()
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
		return
	}

	ts.Fatalf("wait-for: timeout waiting for condition: %s %s", condition, target)
}

// cmdDockerRun implements the 'docker-run' command for testscript.
// It runs a command inside a Docker container.
// Usage:
//
//	docker-run <image> <command> [args...]
//
// The container is run with:
//   - --rm (auto-remove after exit)
//   - --add-host=host.docker.internal:host-gateway (for Linux compatibility)
//   - Working directory mounted if needed
func (h *Harness) cmdDockerRun(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("docker-run: usage: docker-run <image> <command> [args...]")
	}

	var (
		image         = os.Expand(args[0], ts.Getenv)
		containerArgs = make([]string, len(args)-1)
	)

	for i, arg := range args[1:] {
		containerArgs[i] = os.Expand(arg, ts.Getenv)
	}

	// Build docker run command
	dockerArgs := []string{
		"run", "--rm",
		"--add-host=host.docker.internal:host-gateway",
		image,
	}
	dockerArgs = append(dockerArgs, containerArgs...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Stdout = ts.Stdout()
	cmd.Stderr = ts.Stderr()

	err := cmd.Run()
	if neg {
		if err == nil {
			ts.Fatalf("docker-run: command succeeded unexpectedly")
		}
		return
	}

	if err != nil {
		ts.Fatalf("docker-run: command failed: %v", err)
	}
}

// =============================================================================
// Registry commands
// =============================================================================

// cmdRegistryStart starts a test registry container.
// The registry is automatically cleaned up when the test ends.
// Usage: registry-start
// Exports $TEST_REGISTRY environment variable with the registry address.
func (h *Harness) cmdRegistryStart(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("registry-start: does not support negation")
	}

	workDir := ts.Getenv("WORK")

	// Check if registry is already running (idempotent)
	h.registriesMu.Lock()
	if info, exists := h.registries[workDir]; exists {
		h.registriesMu.Unlock()
		// Already started, just ensure env is set
		ts.Setenv("TEST_REGISTRY", info.host)
		return
	}
	h.registriesMu.Unlock()

	// Start new registry
	container, cleanup, err := registry_testhelpers.StartTestRegistryWithCleanup(context.Background())
	if err != nil {
		ts.Fatalf("registry-start: failed to start registry: %v", err)
	}

	host := container.RegistryHost()

	// Store for cleanup
	h.registriesMu.Lock()
	h.registries[workDir] = &registryInfo{
		container: container,
		cleanup:   cleanup,
		host:      host,
	}
	h.registriesMu.Unlock()

	ts.Setenv("TEST_REGISTRY", host)
	ts.Logf("registry-start: started registry at %s", host)
}

// stopRegistryByWorkDir stops the registry container associated with a work directory.
func (h *Harness) stopRegistryByWorkDir(workDir string) {
	h.registriesMu.Lock()
	info, exists := h.registries[workDir]
	if !exists {
		h.registriesMu.Unlock()
		return
	}
	delete(h.registries, workDir)
	h.registriesMu.Unlock()

	if info.cleanup != nil {
		info.cleanup()
	}
}

// cmdRegistryInspect inspects a registry manifest and outputs JSON.
// Usage: registry-inspect <image-ref>
// Outputs the manifest result as JSON to stdout.
func (h *Harness) cmdRegistryInspect(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 1 {
		ts.Fatalf("registry-inspect: usage: registry-inspect <image-ref>")
	}

	imageRef := os.Expand(args[0], ts.Getenv)

	client := registry.NewRegistryClient()
	result, err := client.Inspect(context.Background(), imageRef, nil)

	if neg {
		if err == nil {
			ts.Fatalf("registry-inspect: expected failure but succeeded")
		}
		return
	}

	if err != nil {
		ts.Fatalf("registry-inspect: failed to inspect %s: %v", imageRef, err)
	}

	// Output as JSON
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		ts.Fatalf("registry-inspect: failed to marshal result: %v", err)
	}

	_, _ = ts.Stdout().Write(output)
	_, _ = ts.Stdout().Write([]byte("\n"))
}

// cmdDockerPush tags and pushes a local image to the test registry.
// Usage: docker-push <local-image> <registry-repo:tag>
// Example: docker-push $TEST_IMAGE test/mymodel:v1
// The image is pushed to $TEST_REGISTRY/<registry-repo:tag>
func (h *Harness) cmdDockerPush(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("docker-push: usage: docker-push <local-image> <registry-repo:tag>")
	}

	localImage := os.Expand(args[0], ts.Getenv)
	repoTag := os.Expand(args[1], ts.Getenv)

	testRegistry := ts.Getenv("TEST_REGISTRY")
	if testRegistry == "" {
		ts.Fatalf("docker-push: TEST_REGISTRY not set (call registry-start first)")
	}

	remoteRef := testRegistry + "/" + repoTag

	// Tag the image
	tagCmd := exec.Command("docker", "tag", localImage, remoteRef)
	tagCmd.Stdout = ts.Stdout()
	tagCmd.Stderr = ts.Stderr()
	if err := tagCmd.Run(); err != nil {
		if neg {
			return
		}
		ts.Fatalf("docker-push: failed to tag image: %v", err)
	}

	// Push the image
	pushCmd := exec.Command("docker", "push", remoteRef)
	pushCmd.Stdout = ts.Stdout()
	pushCmd.Stderr = ts.Stderr()
	err := pushCmd.Run()

	if neg {
		if err == nil {
			ts.Fatalf("docker-push: expected failure but succeeded")
		}
		return
	}

	if err != nil {
		ts.Fatalf("docker-push: failed to push image: %v", err)
	}

	ts.Logf("docker-push: pushed %s to %s", localImage, remoteRef)
}

// =============================================================================
// Mock weights command
// =============================================================================

// mockWeightsLock mirrors the structure from pkg/model/weights_lock.go
// SYNC: If pkg/model/WeightsLock changes, update this copy.
// We duplicate it here to avoid importing pkg/model which transitively imports pkg/wheels.
type mockWeightsLock struct {
	Version string           `json:"version"`
	Created time.Time        `json:"created"`
	Files   []mockWeightFile `json:"files"`
}

// mockWeightFile mirrors WeightFile from pkg/model/weights.go
// SYNC: If pkg/model/WeightFile changes, update this copy.
type mockWeightFile struct {
	Name             string `json:"name"`
	Dest             string `json:"dest"`
	DigestOriginal   string `json:"digestOriginal"`
	Digest           string `json:"digest"`
	Size             int64  `json:"size"`
	SizeUncompressed int64  `json:"sizeUncompressed"`
	MediaType        string `json:"mediaType"`
	ContentType      string `json:"contentType,omitempty"`
}

// cmdMockWeights generates mock weight files and a weights.lock file.
// Usage: mock-weights [--count N] [--min-size S] [--max-size S]
// Defaults:
//   - count: 2
//   - min-size: 1kb
//   - max-size: 10kb
//
// Creates files in $WORK/weights/ and writes $WORK/weights.lock
func (h *Harness) cmdMockWeights(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("mock-weights: does not support negation")
	}

	// Parse arguments
	count := 2
	minSize := int64(1024)      // 1KB
	maxSize := int64(10 * 1024) // 10KB

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--count", "-n":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					count = n
				}
				i++
			}
		case "--min-size":
			if i+1 < len(args) {
				if size, err := parseSize(args[i+1]); err == nil {
					minSize = size
				}
				i++
			}
		case "--max-size":
			if i+1 < len(args) {
				if size, err := parseSize(args[i+1]); err == nil {
					maxSize = size
				}
				i++
			}
		}
	}

	workDir := ts.Getenv("WORK")
	weightsDir := filepath.Join(workDir, "weights")
	lockPath := filepath.Join(workDir, "weights.lock")

	// Create weights directory
	if err := os.MkdirAll(weightsDir, 0o755); err != nil {
		ts.Fatalf("mock-weights: failed to create weights dir: %v", err)
	}

	var files []mockWeightFile

	for i := 1; i <= count; i++ {
		// Random size between min and max
		size := minSize
		if maxSize > minSize {
			size = minSize + mathrand.Int64N(maxSize-minSize+1) //nolint:gosec // test data, not security-sensitive
		}

		// Generate identifier (e.g., "weights-001")
		weightName := fmt.Sprintf("weights-%03d", i)
		filename := weightName + ".bin"
		filePath := filepath.Join(weightsDir, filename)

		// Generate random data
		data := make([]byte, size)
		if _, err := cryptorand.Read(data); err != nil {
			ts.Fatalf("mock-weights: failed to generate random data: %v", err)
		}

		// Write file
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			ts.Fatalf("mock-weights: failed to write %s: %v", filename, err)
		}

		// Compute digest (uncompressed, since we're not actually compressing for tests)
		hash := sha256.Sum256(data)
		digest := "sha256:" + hex.EncodeToString(hash[:])

		files = append(files, mockWeightFile{
			Name:             weightName,
			Dest:             "/cache/" + filename,
			DigestOriginal:   digest,
			Digest:           digest, // Same as original since we're not compressing
			Size:             size,
			SizeUncompressed: size,
			// MediaType matches production WeightBuilder output (uncompressed).
			MediaType:   "application/vnd.cog.weight.layer.v1",
			ContentType: "application/octet-stream",
		})
	}

	// Create weights.lock
	lock := mockWeightsLock{
		Version: "1.0",
		Created: time.Now().UTC(),
		Files:   files,
	}

	lockData, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		ts.Fatalf("mock-weights: failed to marshal weights.lock: %v", err)
	}

	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		ts.Fatalf("mock-weights: failed to write weights.lock: %v", err)
	}

	ts.Logf("mock-weights: created %d files in %s", count, weightsDir)
}

// parseSize parses size strings like "1kb", "10KB", "1mb" into bytes.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(s, "gb"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "gb")
	case strings.HasSuffix(s, "mb"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "mb")
	case strings.HasSuffix(s, "kb"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "kb")
	case strings.HasSuffix(s, "b"):
		numStr = strings.TrimSuffix(s, "b")
	default:
		numStr = s
	}

	num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", numStr)
	}
	if num < 0 {
		return 0, fmt.Errorf("size cannot be negative")
	}

	return int64(num * float64(multiplier)), nil
}

// =============================================================================
// Mock upload server commands
// =============================================================================

// cmdUploadServerStart starts a mock HTTP upload server on the host.
// It accepts PUT requests, records them, and responds with a Location header.
// Usage: upload-server-start
// Exports $UPLOAD_SERVER_URL with the server's base URL.
func (h *Harness) cmdUploadServerStart(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("upload-server-start: does not support negation")
	}

	workDir := ts.Getenv("WORK")

	h.uploadServersMu.Lock()
	if _, exists := h.uploadServers[workDir]; exists {
		h.uploadServersMu.Unlock()
		ts.Fatalf("upload-server-start: server already running for this test")
	}
	h.uploadServersMu.Unlock()

	mus := &mockUploadServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		record := mockUploadRecord{
			Path:        r.URL.Path,
			ContentType: r.Header.Get("Content-Type"),
			Size:        len(body),
		}
		mus.mu.Lock()
		mus.uploads = append(mus.uploads, record)
		mus.mu.Unlock()

		// Return a clean URL without query params (simulates a signed URL redirect)
		location := fmt.Sprintf("http://host.docker.internal:%d%s", mus.port, r.URL.Path)
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusOK)
	})

	mus.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second} //nolint:gosec // test harness, not production

	// Bind to all interfaces so the container can reach us via host.docker.internal
	ln, err := net.Listen("tcp", "0.0.0.0:0") //nolint:gosec // must be reachable from Docker container
	if err != nil {
		ts.Fatalf("upload-server-start: failed to listen: %v", err)
	}
	mus.port = ln.Addr().(*net.TCPAddr).Port

	go func() { _ = mus.server.Serve(ln) }()

	h.uploadServersMu.Lock()
	h.uploadServers[workDir] = mus
	h.uploadServersMu.Unlock()

	// Advertise host.docker.internal so the container can reach the host server.
	// On Linux, cog serve adds --add-host=host.docker.internal:host-gateway.
	// On Mac, Docker Desktop resolves host.docker.internal automatically.
	url := fmt.Sprintf("http://host.docker.internal:%d/", mus.port)
	ts.Setenv("UPLOAD_SERVER_URL", url)
	ts.Logf("upload-server-start: listening on 0.0.0.0:%d, container URL: %s", mus.port, url)
}

// cmdUploadServerCount verifies exactly N uploads were received.
// Usage: upload-server-count N
func (h *Harness) cmdUploadServerCount(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 1 {
		ts.Fatalf("upload-server-count: usage: upload-server-count N")
	}

	expected, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("upload-server-count: invalid count %q: %v", args[0], err)
	}

	workDir := ts.Getenv("WORK")
	h.uploadServersMu.Lock()
	mus, exists := h.uploadServers[workDir]
	h.uploadServersMu.Unlock()

	if !exists {
		ts.Fatalf("upload-server-count: no upload server running (call upload-server-start first)")
	}

	mus.mu.Lock()
	got := len(mus.uploads)
	mus.mu.Unlock()

	if neg {
		if got == expected {
			ts.Fatalf("upload-server-count: expected NOT %d uploads but got %d", expected, got)
		}
		return
	}

	if got != expected {
		ts.Fatalf("upload-server-count: expected %d uploads but got %d", expected, got)
	}
}

// stopUploadServerByWorkDir shuts down the upload server for a work directory.
func (h *Harness) stopUploadServerByWorkDir(workDir string) {
	h.uploadServersMu.Lock()
	mus, exists := h.uploadServers[workDir]
	if !exists {
		h.uploadServersMu.Unlock()
		return
	}
	delete(h.uploadServers, workDir)
	h.uploadServersMu.Unlock()

	if mus.server != nil {
		_ = mus.server.Close()
	}
}

// =============================================================================
// Webhook receiver commands
// =============================================================================

// cmdWebhookServerStart starts a webhook receiver that accepts prediction callbacks.
// It parses the JSON payload to extract status and measure the output size, without
// ever exposing the (potentially huge) output to testscript's log buffer.
// Usage: webhook-server-start
// Exports $WEBHOOK_URL with the server's callback URL.
func (h *Harness) cmdWebhookServerStart(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("webhook-server-start: does not support negation")
	}

	workDir := ts.Getenv("WORK")

	h.webhookServersMu.Lock()
	if _, exists := h.webhookServers[workDir]; exists {
		h.webhookServersMu.Unlock()
		ts.Fatalf("webhook-server-start: server already running for this test")
	}
	h.webhookServersMu.Unlock()

	ws := &webhookServer{
		done: make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Stream-parse the JSON to extract status, measure output size, and
		// capture metrics without holding the entire output string in memory.
		var payload struct {
			Status  string          `json:"status"`
			Output  string          `json:"output"`
			Error   string          `json:"error"`
			Metrics json.RawMessage `json:"metrics"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)

		// Only record terminal statuses
		switch payload.Status {
		case "succeeded", "failed", "canceled":
		default:
			return
		}

		ws.mu.Lock()
		defer ws.mu.Unlock()

		// Only record the first terminal callback
		if ws.result != nil {
			return
		}
		ws.result = &webhookResult{
			Status:     payload.Status,
			OutputSize: len(payload.Output),
			HasError:   payload.Error != "",
			Metrics:    payload.Metrics,
		}
		close(ws.done)
	})

	ws.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second} //nolint:gosec

	// Bind to all interfaces so the container can reach us via host.docker.internal
	ln, err := net.Listen("tcp", "0.0.0.0:0") //nolint:gosec
	if err != nil {
		ts.Fatalf("webhook-server-start: failed to listen: %v", err)
	}
	ws.port = ln.Addr().(*net.TCPAddr).Port

	go func() { _ = ws.server.Serve(ln) }()

	h.webhookServersMu.Lock()
	h.webhookServers[workDir] = ws
	h.webhookServersMu.Unlock()

	url := fmt.Sprintf("http://host.docker.internal:%d/webhook", ws.port)
	ts.Setenv("WEBHOOK_URL", url)
	ts.Logf("webhook-server-start: listening on 0.0.0.0:%d, container URL: %s", ws.port, url)
}

// cmdWebhookServerWait blocks until the webhook server receives a terminal prediction callback,
// then writes a compact JSON summary to stdout for assertion with stdout/stderr matchers.
// Usage: webhook-server-wait [timeout]
// Default timeout: 120s
func (h *Harness) cmdWebhookServerWait(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("webhook-server-wait: does not support negation")
	}

	timeout := 120 * time.Second
	if len(args) > 0 {
		if d, err := time.ParseDuration(args[0]); err == nil {
			timeout = d
		}
	}

	workDir := ts.Getenv("WORK")
	h.webhookServersMu.Lock()
	ws, exists := h.webhookServers[workDir]
	h.webhookServersMu.Unlock()

	if !exists {
		ts.Fatalf("webhook-server-wait: no webhook server running (call webhook-server-start first)")
	}

	select {
	case <-ws.done:
	case <-time.After(timeout):
		ts.Fatalf("webhook-server-wait: timed out after %s waiting for terminal webhook", timeout)
	}

	ws.mu.Lock()
	result := ws.result
	ws.mu.Unlock()

	out, _ := json.Marshal(result)
	_, _ = ts.Stdout().Write(out)
	_, _ = ts.Stdout().Write([]byte("\n"))
}

// stopWebhookServerByWorkDir shuts down the webhook server for a work directory.
func (h *Harness) stopWebhookServerByWorkDir(workDir string) {
	h.webhookServersMu.Lock()
	ws, exists := h.webhookServers[workDir]
	if !exists {
		h.webhookServersMu.Unlock()
		return
	}
	delete(h.webhookServers, workDir)
	h.webhookServersMu.Unlock()

	if ws.server != nil {
		_ = ws.server.Close()
	}
}
