package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
)

func TestLogs(t *testing.T) {
	t.Skip("TODO: Reimplement this test with the new log processor. The test as is, is a tombstone for us to reference the desired behaviors.")
	// TODO: Assess if we need this test, we're testing replicate/go/logging not anything
	// in cog-runtime
	if *legacyCog {
		// Testing the legacy cog server's logging doesn't really inform us about the intended
		// behaviors. If we capture STDOUT and STDERR and validate they're available that is more
		// than sufficient.
		t.Skip("log tests for legacy cog server do not provide useful signal that we're handling logging in coglet.")
	}

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	or, ow, err := os.Pipe()
	require.NoError(t, err)
	er, ew, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = ow
	os.Stderr = ew

	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		or.Close()
		er.Close()
		ow.Close()
		ew.Close()
	})

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "logs",
		predictorClass:   "Predictor",
		envSet: map[string]string{
			"LOG_FILE": "stderr",
		},
	})
	hc := waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)
	assert.Equal(t, "STDOUT: starting setup\nSTDERR: starting setup\nSTDOUT: completed setup\nSTDERR: completed setup\n", hc.Setup.Logs)

	prediction := runner.PredictionRequest{Input: map[string]any{"s": "bar"}}
	req := httpPredictionRequest(t, runtimeServer, prediction)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse runner.PredictionResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
	assert.Equal(t, "*bar*", predictionResponse.Output)
	assert.Equal(t, "STDOUT: starting prediction\nSTDERR: starting prediction\n[NOT_A_PID] STDOUT not a prediction ID\n[NOT_A_PID] STDERR not a prediction ID\nSTDOUT: completed prediction\nSTDERR: completed prediction\n", predictionResponse.Logs)

	// NOTE: Force a flush and make the readers available for reading
	ow.Close()
	ew.Close()

	// COPY stdout from pipe to buffer for asserting.
	_, err = io.Copy(stdout, or)
	require.NoError(t, err)
	_, err = io.Copy(stderr, er)
	require.NoError(t, err)

	assert.Equal(t, "STDOUT: starting setup\nSTDOUT: completed setup\nSTDOUT: starting prediction\n[NOT_A_PID] STDOUT not a prediction ID\nSTDOUT: completed prediction\n", stdout.String())
	stderrLines := strings.Split(stderr.String(), "\n")
	assert.Contains(t, stderrLines, "STDERR: starting setup")
	assert.Contains(t, stderrLines, "STDERR: completed setup")
	assert.Contains(t, stderrLines, "STDERR: starting prediction")
	assert.Contains(t, stderrLines, "[NOT_A_PID] STDERR not a prediction ID")
	assert.Contains(t, stderrLines, "STDERR: completed prediction")
}
