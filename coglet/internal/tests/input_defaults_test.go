package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
)

func TestInputDefaults(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("Mutable default validation is coglet specific.")
	}

	t.Run("mutable default auto-converts", func(t *testing.T) {
		runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
			procedureMode:    false,
			explicitShutdown: false,
			uploadURL:        "",
			module:           "input_bad_mutable_default",
			predictorClass:   "Predictor",
		})

		// Wait for setup to complete, expecting it to succeed due to auto-conversion
		waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

		// Verify that the predictor actually works with auto-converted default
		input := map[string]any{} // Use default
		req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var prediction testHarnessResponse
		err = json.Unmarshal(body, &prediction)
		require.NoError(t, err)

		assert.Equal(t, runner.PredictionSucceeded, prediction.Status)
		assert.Equal(t, "items: [1, 2, 3]", prediction.Output)
	})

	t.Run("immutable default succeeds", func(t *testing.T) {
		t.Parallel()

		runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
			procedureMode:    false,
			explicitShutdown: false,
			uploadURL:        "",
			module:           "input_immutable_default",
			predictorClass:   "Predictor",
		})

		// Wait for setup to complete successfully
		waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

		// Verify that the predictor actually works
		input := map[string]any{} // Use default
		req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var prediction testHarnessResponse
		err = json.Unmarshal(body, &prediction)
		require.NoError(t, err)

		assert.Equal(t, runner.PredictionSucceeded, prediction.Status)
		assert.Equal(t, "message: hello world", prediction.Output)
	})

	t.Run("immutable default with overrided value succeeds", func(t *testing.T) {
		t.Parallel()

		runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
			procedureMode:    false,
			explicitShutdown: false,
			uploadURL:        "",
			module:           "input_immutable_default",
			predictorClass:   "Predictor",
		})

		waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

		// Test with custom input
		input := map[string]any{"message": "custom message"}
		req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var prediction testHarnessResponse
		err = json.Unmarshal(body, &prediction)
		require.NoError(t, err)

		assert.Equal(t, runner.PredictionSucceeded, prediction.Status)
		assert.Equal(t, "message: custom message", prediction.Output)
	})

	t.Run("mutable default isolation", func(t *testing.T) {
		t.Parallel()

		runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
			procedureMode:    false,
			explicitShutdown: false,
			uploadURL:        "",
			module:           "input_mutable_isolation",
			predictorClass:   "Predictor",
		})

		// Wait for setup to complete successfully
		waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

		// First prediction call - mutates the default list
		input1 := map[string]any{} // Use default
		req1 := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input1})

		resp1, err := http.DefaultClient.Do(req1)
		require.NoError(t, err)
		defer resp1.Body.Close()
		assert.Equal(t, http.StatusOK, resp1.StatusCode)

		body1, err := io.ReadAll(resp1.Body)
		require.NoError(t, err)

		var prediction1 testHarnessResponse
		err = json.Unmarshal(body1, &prediction1)
		require.NoError(t, err)

		assert.Equal(t, runner.PredictionSucceeded, prediction1.Status)
		assert.Equal(t, "items: [1, 2, 3, 999]", prediction1.Output)

		// Wait for runner to be ready for next prediction
		waitForReady(t, runtimeServer)

		// Second prediction call - should get fresh default, not mutated version
		input2 := map[string]any{} // Use default again
		req2 := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input2})

		resp2, err := http.DefaultClient.Do(req2)
		require.NoError(t, err)
		defer resp2.Body.Close()

		body2, err := io.ReadAll(resp2.Body)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp2.StatusCode)

		var prediction2 testHarnessResponse
		err = json.Unmarshal(body2, &prediction2)
		require.NoError(t, err)

		assert.Equal(t, runner.PredictionSucceeded, prediction2.Status)
		// This should be the original default, not the mutated version
		assert.Equal(t, "items: [1, 2, 3, 999]", prediction2.Output)
	})
}
