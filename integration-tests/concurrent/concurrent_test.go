package concurrent_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	if os.Getenv("COG_TEST_FAST") == "1" {
		t.Skip("skipping slow test in fast mode")
	}

	// Create a temp directory for our test project
	tmpDir, err := os.MkdirTemp("", "cog-concurrent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write the async-sleep predictor fixture
	if err := os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(cogYAML), 0644); err != nil {
		t.Fatalf("failed to write cog.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "predict.py"), []byte(predictPy), 0644); err != nil {
		t.Fatalf("failed to write predict.py: %v", err)
	}

	// Get the cog binary
	cogBinary, err := harness.ResolveCogBinary()
	if err != nil {
		t.Fatalf("failed to resolve cog binary: %v", err)
	}

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
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build image: %v\n%s", err, output)
	}

	// Start the server
	t.Log("Starting server...")
	port := 5555 // Use a fixed port for simplicity
	serveCmd := exec.Command(cogBinary, "serve", "-p", fmt.Sprintf("%d", port))
	serveCmd.Dir = tmpDir
	serveCmd.Env = append(os.Environ(), "COG_NO_UPDATE_CHECK=1")

	if err := serveCmd.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		serveCmd.Process.Kill()
		serveCmd.Wait()
	}()

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait for server to be ready
	t.Log("Waiting for server to be ready...")
	if !waitForServerReady(serverURL, 60*time.Second) {
		t.Fatal("server did not become ready within timeout")
	}

	// Fire 5 concurrent predictions
	t.Log("Starting concurrent predictions...")
	const numPredictions = 5
	var wg sync.WaitGroup
	results := make([]predictionResult, numPredictions)

	start := time.Now()

	for i := 0; i < numPredictions; i++ {
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
	if elapsed >= 3*time.Second {
		t.Errorf("predictions took too long (%v), expected < 3s for concurrent execution", elapsed)
	}

	// Verify all predictions succeeded with correct output
	for i, result := range results {
		if result.err != nil {
			t.Errorf("prediction %d failed: %v", i, result.err)
			continue
		}
		if result.statusCode != 200 {
			t.Errorf("prediction %d returned status %d, want 200", i, result.statusCode)
			continue
		}
		expectedOutput := fmt.Sprintf("wake up sleepyhead%d", i)
		if result.output != expectedOutput {
			t.Errorf("prediction %d output = %q, want %q", i, result.output, expectedOutput)
		}
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
		bytes.NewBufferString(reqBody),
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

// Embedded fixture files

const cogYAML = `build:
  python_version: "3.11"
predict: "predict.py:Predictor"
concurrency:
  max: 5
`

const predictPy = `import asyncio
from cog import BasePredictor


class Predictor(BasePredictor):
    async def predict(self, s: str, sleep: float) -> str:
        await asyncio.sleep(sleep)
        return f"wake up {s}"
`
