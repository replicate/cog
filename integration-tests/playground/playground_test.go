//go:build integration

// Package playground provides integration tests for the cog playground command.
//
// These tests verify that the playground web server correctly reverse-proxies
// requests to a running Cog model and relays the model's responses back to the
// caller. They are written in Go (not txtar) because they require starting and
// coordinating two long-running processes (cog serve and cog playground) and
// comparing direct and proxied HTTP responses.
package playground_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/integration-tests/harness"
)

const (
	cogYAML = `build:
  python_version: "3.12"
run: "run.py:Runner"
`

	runPy = `from cog import BaseRunner


class Runner(BaseRunner):
    def run(self, s: str) -> str:
        return "hello " + s
`
)

// TestPlaygroundIntegration builds a simple model, starts cog serve, and then
// starts cog playground. It verifies that requests sent through the
// playground's /proxy path reach the model and that the response body and
// status match a direct request to the model.
func TestPlaygroundIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "cog-playground-test-*")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	err = os.WriteFile(filepath.Join(tmpDir, "cog.yaml"), []byte(cogYAML), 0o644)
	require.NoError(t, err, "failed to write cog.yaml")
	err = os.WriteFile(filepath.Join(tmpDir, "run.py"), []byte(runPy), 0o644)
	require.NoError(t, err, "failed to write run.py")

	cogBinary, err := harness.ResolveCogBinary()
	require.NoError(t, err, "failed to resolve cog binary")

	imageName := fmt.Sprintf("cog-playground-test-%d", time.Now().UnixNano())
	defer func() {
		exec.Command("docker", "rmi", "-f", imageName).Run()
	}()

	t.Log("Building image...")
	buildCmd := exec.Command(cogBinary, "build", "-t", imageName)
	buildCmd.Dir = tmpDir
	buildCmd.Env = testEnv()
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "failed to build image\n%s", output)

	t.Log("Starting cog serve...")
	servePort, err := allocatePort()
	require.NoError(t, err, "failed to allocate serve port")

	serveCmd := exec.Command(cogBinary, "serve", "-p", fmt.Sprintf("%d", servePort))
	serveCmd.Dir = tmpDir
	serveCmd.Env = testEnv()
	err = serveCmd.Start()
	require.NoError(t, err, "failed to start cog serve")
	defer func() {
		serveCmd.Process.Kill()
		serveCmd.Wait()
	}()

	modelURL := fmt.Sprintf("http://127.0.0.1:%d", servePort)
	require.True(t, waitForServerReady(modelURL, 120*time.Second), "model server did not become ready within timeout")

	t.Log("Starting cog playground...")
	playgroundPort, err := allocatePort()
	require.NoError(t, err, "failed to allocate playground port")

	playgroundCmd := exec.Command(
		cogBinary,
		"playground",
		"--no-open",
		"--target", modelURL,
		"--port", fmt.Sprintf("%d", playgroundPort),
	)
	playgroundCmd.Env = testEnv()
	err = playgroundCmd.Start()
	require.NoError(t, err, "failed to start cog playground")
	defer func() {
		playgroundCmd.Process.Kill()
		playgroundCmd.Wait()
	}()

	playgroundURL := fmt.Sprintf("http://127.0.0.1:%d", playgroundPort)
	require.True(t, waitForPlaygroundReady(playgroundURL, 30*time.Second), "playground did not become ready within timeout")

	t.Log("Verifying health-check proxying...")
	directHealth := mustGet(t, modelURL+"/health-check")
	proxiedHealth := mustProxyGet(t, playgroundURL, modelURL, "/health-check")
	assert.Equal(t, http.StatusOK, proxiedHealth.StatusCode, "proxied health-check status")
	assert.JSONEq(t, directHealth.Body, proxiedHealth.Body, "proxied health-check body should match direct response")

	t.Log("Verifying prediction proxying...")
	body := `{"input":{"s":"world"}}`
	directPred := mustPost(t, modelURL+"/predictions", body)
	proxiedPred := mustProxyPost(t, playgroundURL, modelURL, "/predictions", body)
	assert.Equal(t, directPred.StatusCode, proxiedPred.StatusCode, "proxied prediction status should match direct response")

	var directResp, proxiedResp predictionResponse
	require.NoError(t, json.Unmarshal([]byte(directPred.Body), &directResp))
	require.NoError(t, json.Unmarshal([]byte(proxiedPred.Body), &proxiedResp))
	assert.Equal(t, "succeeded", proxiedResp.Status, "proxied prediction should succeed")
	assert.Equal(t, "hello world", proxiedResp.Output, "proxied prediction output should match model output")
	assert.Equal(t, directResp.Status, proxiedResp.Status, "proxied prediction status should match direct response")
	assert.Equal(t, directResp.Output, proxiedResp.Output, "proxied prediction output should match direct response")
}

type httpResponse struct {
	StatusCode int
	Body       string
}

type predictionResponse struct {
	Status string `json:"status"`
	Output string `json:"output"`
}

func mustGet(t *testing.T, url string) httpResponse {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err, "GET %s failed", url)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "reading GET %s body failed", url)
	return httpResponse{StatusCode: resp.StatusCode, Body: string(body)}
}

func mustPost(t *testing.T, url, body string) httpResponse {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	require.NoError(t, err, "POST %s failed", url)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "reading POST %s body failed", url)
	return httpResponse{StatusCode: resp.StatusCode, Body: string(respBody)}
}

func mustProxyGet(t *testing.T, playgroundURL, targetURL, path string) httpResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, playgroundURL+"/proxy"+path, nil)
	require.NoError(t, err, "creating proxy request failed")
	req.Header.Set("X-Cog-Target", targetURL)
	return doRequest(t, req)
}

func mustProxyPost(t *testing.T, playgroundURL, targetURL, path, body string) httpResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, playgroundURL+"/proxy"+path, bytes.NewReader([]byte(body)))
	require.NoError(t, err, "creating proxy request failed")
	req.Header.Set("X-Cog-Target", targetURL)
	req.Header.Set("Content-Type", "application/json")
	return doRequest(t, req)
}

func doRequest(t *testing.T, req *http.Request) httpResponse {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "request to %s failed", req.URL)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "reading response body from %s failed", req.URL)
	return httpResponse{StatusCode: resp.StatusCode, Body: string(body)}
}

// testEnv returns the host environment with COG_NO_UPDATE_CHECK set and
// COG_CA_CERT removed. The latter may be set in local developer environments
// to a file path that does not exist in the test's context, which breaks
// Docker builds.
func testEnv() []string {
	env := os.Environ()
	var filtered []string
	for _, e := range env {
		if strings.HasPrefix(e, "COG_CA_CERT=") {
			continue
		}
		filtered = append(filtered, e)
	}
	return append(filtered, "COG_NO_UPDATE_CHECK=1")
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

// waitForServerReady polls the Cog model server's health-check endpoint until
// it reports READY.
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

// waitForPlaygroundReady polls the playground's /config endpoint until it
// responds successfully.
func waitForPlaygroundReady(playgroundURL string, timeout time.Duration) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(playgroundURL + "/config")
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}

	return false
}
