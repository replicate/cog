//go:build integration

package concurrent_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/integration-tests/harness"
)

// TestConcurrentPredictions tests that concurrent async predictions complete properly
// with server shutdown.
//
// This test verifies:
// 1. Multiple predictions can run concurrently
// 2. Server shutdown waits for running predictions to complete
// 3. All predictions return correct results
//
// This test is written in Go (not txtar) because it requires parallel HTTP requests
// with precise timing coordination that doesn't fit txtar's sequential execution model.
func TestConcurrentPredictions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	// Create a temp directory for our test project
	tmpDir, err := os.MkdirTemp("", "cog-concurrent-test-*")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	// Write the async-sleep runner fixture
	err = os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(cogYAML), 0o644)
	require.NoError(t, err, "failed to write cog.yaml")
	err = os.WriteFile(filepath.Join(tmpDir, "run.py"), []byte(runPy), 0o644)
	require.NoError(t, err, "failed to write run.py")

	// Get the cog binary
	cogBinary, err := harness.ResolveCogBinary()
	require.NoError(t, err, "failed to resolve cog binary")

	// Generate unique image name
	imageName := fmt.Sprintf("cog-concurrent-test-%d", time.Now().UnixNano())
	defer func() {
		exec.Command("docker", "rmi", "-f", imageName).Run()
	}()

	// Build the image
	t.Log("Building image...")
	buildCmd := exec.Command(cogBinary, "build", "-t", imageName)
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "failed to build image\n%s", output)

	// Start the server
	t.Log("Starting server...")
	port, err := allocatePort()
	require.NoError(t, err, "failed to allocate port")

	serveCmd := exec.Command(cogBinary, "serve", "-p", fmt.Sprintf("%d", port))
	serveCmd.Dir = tmpDir
	serveCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	err = serveCmd.Start()
	require.NoError(t, err, "failed to start server")
	defer func() {
		serveCmd.Process.Kill()
		serveCmd.Wait()
	}()

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait for server to be ready
	t.Log("Waiting for server to be ready...")
	require.True(t, waitForServerReady(serverURL, 60*time.Second), "server did not become ready within timeout")

	// Fire 5 concurrent predictions
	t.Log("Starting concurrent predictions...")
	const numPredictions = 5
	var wg sync.WaitGroup
	results := make([]predictionResult, numPredictions)

	start := time.Now()

	for i := range numPredictions {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = makePrediction(serverURL, idx)
		}(i)
	}

	// Wait a bit for all predictions to be accepted but not completed
	time.Sleep(200 * time.Millisecond)

	// Shutdown the server while predictions are in-flight
	t.Log("Sending shutdown request...")
	shutdownResp, err := http.Post(serverURL+"/shutdown", "application/json", nil)
	if err != nil {
		t.Logf("shutdown request error (may be expected): %v", err)
	} else {
		shutdownResp.Body.Close()
	}

	// Wait for all predictions to complete
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("All predictions completed in %v", elapsed)

	// Verify timing - should be < 3s if running concurrently (each sleeps 1s)
	assert.Less(t, elapsed, 3*time.Second, "predictions took too long (%v), expected < 3s for concurrent execution", elapsed)

	// Verify all predictions succeeded with correct output
	for i, result := range results {
		if !assert.NoError(t, result.err, "prediction %d failed", i) {
			continue
		}
		if !assert.Equal(t, http.StatusOK, result.statusCode, "prediction %d returned unexpected status", i) {
			continue
		}
		expectedOutput := fmt.Sprintf("wake up sleepyhead%d", i)
		assert.Equal(t, expectedOutput, result.output, "prediction %d output mismatch", i)
	}
}

type predictionResult struct {
	statusCode int
	output     string
	err        error
}

func makePrediction(serverURL string, idx int) predictionResult {
	reqBody := fmt.Sprintf(`{"id":"id-%d","input":{"s":"sleepyhead%d","sleep":1.0}}`, idx, idx)

	resp, err := http.Post(
		serverURL+"/predictions",
		"application/json",
		strings.NewReader(reqBody),
	)
	if err != nil {
		return predictionResult{err: err}
	}
	defer resp.Body.Close()

	var response struct {
		Output string `json:"output"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return predictionResult{statusCode: resp.StatusCode, err: err}
	}

	return predictionResult{
		statusCode: resp.StatusCode,
		output:     response.Output,
	}
}

func waitForServerReady(serverURL string, timeout time.Duration) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(serverURL + "/health-check")
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		var health struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			resp.Body.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		resp.Body.Close()

		if health.Status == "READY" {
			return true
		}
		if health.Status == "SETUP_FAILED" || health.Status == "DEFUNCT" {
			return false
		}

		time.Sleep(200 * time.Millisecond)
	}

	return false
}

// waitForServerStatus polls /health-check until the server reports the given status.
// Unlike waitForServerReady which waits for READY, this can wait for intermediate
// states like STARTING (useful for testing signals during setup).
func waitForServerStatus(serverURL string, targetStatus string, timeout time.Duration) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(serverURL + "/health-check")
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		var health struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			resp.Body.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		resp.Body.Close()

		if health.Status == targetStatus {
			return true
		}
		if health.Status == "SETUP_FAILED" || health.Status == "DEFUNCT" {
			return false
		}

		time.Sleep(200 * time.Millisecond)
	}

	return false
}

// allocatePort finds an available TCP port by letting the OS assign one.
func allocatePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// Embedded fixture files

const cogYAML = `build:
  python_version: "3.11"
run: "run.py:Runner"
concurrency:
  max: 5
`

const runPy = `import asyncio
from cog import BaseRunner


class Runner(BaseRunner):
    async def run(self, s: str, sleep: float) -> str:
        await asyncio.sleep(sleep)
        return f"wake up {s}"
`

// TestConcurrentAboveLimit tests that sending more predictions than max_concurrency
// returns a 409 Conflict for the excess prediction.
func TestConcurrentAboveLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "cog-above-limit-test-*")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	err = os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(aboveLimitCogYAML), 0o644)
	require.NoError(t, err, "failed to write cog.yaml")
	err = os.WriteFile(filepath.Join(tmpDir, "run.py"), []byte(runPy), 0o644)
	require.NoError(t, err, "failed to write run.py")

	cogBinary, err := harness.ResolveCogBinary()
	require.NoError(t, err, "failed to resolve cog binary")

	imageName := fmt.Sprintf("cog-above-limit-test-%d", time.Now().UnixNano())
	defer func() {
		exec.Command("docker", "rmi", "-f", imageName).Run()
	}()

	t.Log("Building image...")
	buildCmd := exec.Command(cogBinary, "build", "-t", imageName)
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "failed to build image\n%s", output)

	t.Log("Starting server...")
	port, err := allocatePort()
	require.NoError(t, err, "failed to allocate port")

	serveCmd := exec.Command(cogBinary, "serve", "-p", fmt.Sprintf("%d", port))
	serveCmd.Dir = tmpDir
	serveCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	err = serveCmd.Start()
	require.NoError(t, err, "failed to start server")
	defer func() {
		serveCmd.Process.Kill()
		serveCmd.Wait()
	}()

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	t.Log("Waiting for server to be ready...")
	require.True(t, waitForServerReady(serverURL, 60*time.Second), "server did not become ready within timeout")

	// Fill all 2 slots with long-running predictions (each sleeps 1s)
	const maxConcurrency = 2
	var wg sync.WaitGroup
	for i := range maxConcurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			makePrediction(serverURL, idx)
		}(i)
	}

	// Poll with an overflow request until we get a 409, meaning both slots
	// are occupied. This avoids a fixed sleep that can flake on slow CI.
	t.Log("Polling for 409 (all slots occupied)...")
	deadline := time.Now().Add(10 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		extraBody := `{"id":"extra","input":{"s":"overflow","sleep":1.0}}`
		resp, err = http.Post(
			serverURL+"/predictions",
			"application/json",
			strings.NewReader(extraBody),
		)
		require.NoError(t, err, "failed to send extra prediction")
		if resp.StatusCode == http.StatusConflict {
			break
		}
		// Got 200 — slots weren't full yet, close and retry
		resp.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}
	defer resp.Body.Close()

	require.Equal(t, http.StatusConflict, resp.StatusCode, "extra prediction status = %d, want %d (409 Conflict); slots never filled within timeout", resp.StatusCode, http.StatusConflict)

	var errResp struct {
		Error  string `json:"error"`
		Status string `json:"status"`
	}
	err = json.NewDecoder(resp.Body).Decode(&errResp)
	require.NoError(t, err, "failed to decode error response")
	assert.Equal(t, "failed", errResp.Status, "error response status mismatch")
	assert.Contains(t, strings.ToLower(errResp.Error), "capacity", "error response error = %q, want string containing \"capacity\"", errResp.Error)

	wg.Wait()
}

const aboveLimitCogYAML = `build:
  python_version: "3.11"
run: "run.py:Runner"
concurrency:
  max: 2
`

// TestConcurrentAsyncMetrics tests that metrics recorded via current_scope().record_metric()
// in async run functions are correctly routed to each prediction's response when
// running with concurrency > 1.
//
// This reproduces https://github.com/replicate/cog/issues/2901:
// The metric scope ContextVar is set on the worker thread but async run coroutines
// run on a shared event loop thread where the ContextVar is not propagated. Under
// concurrency > 1, metrics are either silently dropped (noop scope) or attributed to
// the wrong prediction (SYNC_SCOPE race).
func TestConcurrentAsyncMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "cog-async-metrics-test-*")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	metricsCogYAML := `build:
  python_version: "3.12"
run: "run.py:Runner"
concurrency:
  max: 5
`
	metricsRunPy := `import asyncio
from cog import BaseRunner, current_scope


class Runner(BaseRunner):
    async def run(self, idx: int = 0, sleep: float = 0.5) -> str:
        scope = current_scope()
        scope.record_metric("prediction_index", idx)
        scope.record_metric("model_name", "test-model")
        await asyncio.sleep(sleep)
        scope.record_metric("completed", True)
        return f"done-{idx}"
`

	err = os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(metricsCogYAML), 0o644)
	require.NoError(t, err, "failed to write cog.yaml")
	err = os.WriteFile(filepath.Join(tmpDir, "run.py"), []byte(metricsRunPy), 0o644)
	require.NoError(t, err, "failed to write run.py")

	cogBinary, err := harness.ResolveCogBinary()
	require.NoError(t, err, "failed to resolve cog binary")

	imageName := fmt.Sprintf("cog-async-metrics-test-%d", time.Now().UnixNano())
	defer func() {
		exec.Command("docker", "rmi", "-f", imageName).Run()
	}()

	t.Log("Building image...")
	buildCmd := exec.Command(cogBinary, "build", "-t", imageName)
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "failed to build image\n%s", output)

	t.Log("Starting server...")
	port, err := allocatePort()
	require.NoError(t, err, "failed to allocate port")

	serveCmd := exec.Command(cogBinary, "serve", "-p", fmt.Sprintf("%d", port))
	serveCmd.Dir = tmpDir
	serveCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	err = serveCmd.Start()
	require.NoError(t, err, "failed to start server")
	defer func() {
		serveCmd.Process.Kill()
		serveCmd.Wait()
	}()

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	t.Log("Waiting for server to be ready...")
	require.True(t, waitForServerReady(serverURL, 60*time.Second), "server did not become ready within timeout")

	// Fire 5 concurrent predictions, each with a unique index
	const numPredictions = 5
	var wg sync.WaitGroup
	type metricsResult struct {
		statusCode int
		output     string
		metrics    map[string]any
		err        error
	}
	results := make([]metricsResult, numPredictions)

	for i := range numPredictions {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reqBody := fmt.Sprintf(`{"id":"metrics-%d","input":{"idx":%d,"sleep":0.5}}`, idx, idx)
			resp, err := http.Post(
				serverURL+"/predictions",
				"application/json",
				strings.NewReader(reqBody),
			)
			if err != nil {
				results[idx] = metricsResult{err: err}
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				results[idx] = metricsResult{statusCode: resp.StatusCode, err: err}
				return
			}

			var response struct {
				Output  string         `json:"output"`
				Status  string         `json:"status"`
				Metrics map[string]any `json:"metrics"`
			}
			if err := json.Unmarshal(body, &response); err != nil {
				results[idx] = metricsResult{statusCode: resp.StatusCode, err: fmt.Errorf("unmarshal: %w\nbody: %s", err, body)}
				return
			}

			results[idx] = metricsResult{
				statusCode: resp.StatusCode,
				output:     response.Output,
				metrics:    response.Metrics,
			}
		}(i)
	}

	wg.Wait()

	// Verify each prediction has correct, non-cross-contaminated metrics
	for i, result := range results {
		if !assert.NoError(t, result.err, "prediction %d failed", i) {
			continue
		}
		assert.Equal(t, http.StatusOK, result.statusCode, "prediction %d returned unexpected status", i)

		expectedOutput := fmt.Sprintf("done-%d", i)
		assert.Equal(t, expectedOutput, result.output, "prediction %d output mismatch", i)

		// The core assertion: metrics must exist and contain the correct prediction_index.
		// Before the fix, this will either:
		// - Be nil/empty (noop scope — metric silently dropped)
		// - Contain the wrong index (SYNC_SCOPE race — metric attributed to wrong prediction)
		require.NotNil(t, result.metrics, "prediction %d: metrics is nil (scope was not propagated to async coroutine)", i)

		predIdx, ok := result.metrics["prediction_index"]
		require.True(t, ok, "prediction %d: prediction_index metric missing from response metrics: %v", i, result.metrics)
		assert.Equal(t, float64(i), predIdx, "prediction %d: prediction_index metric has wrong value (cross-contamination)", i)

		completed, ok := result.metrics["completed"]
		assert.True(t, ok, "prediction %d: completed metric missing", i)
		assert.Equal(t, true, completed, "prediction %d: completed metric should be true", i)

		modelName, ok := result.metrics["model_name"]
		assert.True(t, ok, "prediction %d: model_name metric missing", i)
		assert.Equal(t, "test-model", modelName, "prediction %d: model_name metric mismatch", i)

		_, hasPredictTime := result.metrics["predict_time"]
		assert.True(t, hasPredictTime, "prediction %d: predict_time system metric missing", i)
	}
}

// TestSIGTERMDuringSetup tests that SIGTERM during setup() causes clean shutdown.
func TestSIGTERMDuringSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "cog-sigterm-setup-test-*")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	slowSetupCogYAML := `build:
  python_version: "3.12"
run: "run.py:Runner"
`
	slowSetupRunPy := `import time
from cog import BaseRunner

class Runner(BaseRunner):
    def setup(self) -> None:
        time.sleep(30)

    def run(self, s: str) -> str:
        return "hello " + s
`

	err = os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(slowSetupCogYAML), 0o644)
	require.NoError(t, err, "failed to write cog.yaml")
	err = os.WriteFile(filepath.Join(tmpDir, "run.py"), []byte(slowSetupRunPy), 0o644)
	require.NoError(t, err, "failed to write run.py")

	cogBinary, err := harness.ResolveCogBinary()
	require.NoError(t, err, "failed to resolve cog binary")

	t.Log("Building image...")
	imageName := fmt.Sprintf("cog-sigterm-setup-test-%d", time.Now().UnixNano())
	defer func() {
		exec.Command("docker", "rmi", "-f", imageName).Run()
	}()

	buildCmd := exec.Command(cogBinary, "build", "-t", imageName)
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "failed to build image\n%s", output)

	t.Log("Starting server...")
	port, err := allocatePort()
	require.NoError(t, err, "failed to allocate port")

	serveCmd := exec.Command(cogBinary, "serve", "-p", fmt.Sprintf("%d", port))
	serveCmd.Dir = tmpDir
	serveCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	err = serveCmd.Start()
	require.NoError(t, err, "failed to start server")

	// Poll health-check until setup has begun (status STARTING),
	// rather than a fixed sleep that can be too short on cold Docker pulls.
	t.Log("Waiting for setup to begin (STARTING status)...")
	if !waitForServerStatus(fmt.Sprintf("http://127.0.0.1:%d", port), "STARTING", 60*time.Second) {
		serveCmd.Process.Kill()
		serveCmd.Wait()
		t.Fatal("server did not reach STARTING status within timeout")
	}

	// Send SIGTERM
	t.Log("Sending SIGTERM during setup...")
	err = serveCmd.Process.Signal(syscall.SIGTERM)
	require.NoError(t, err, "failed to send signal")

	// Wait for process to exit with a timeout
	done := make(chan error, 1)
	go func() {
		done <- serveCmd.Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("server exited cleanly after SIGTERM; expected termination by signal")
		}

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("server exited with unexpected error type after SIGTERM: %T (%v)", err, err)
		}

		ws, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok {
			t.Fatalf("server exited after SIGTERM but wait status was unavailable: %v", err)
		}
		if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
			t.Fatalf("server exit = %v, want signal %v", ws, syscall.SIGTERM)
		}
	case <-time.After(15 * time.Second):
		serveCmd.Process.Kill()
		t.Fatal("server did not exit within 15s after SIGTERM")
	}
}
