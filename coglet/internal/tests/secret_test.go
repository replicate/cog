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

func TestPredictionSecretSucceeded(t *testing.T) {
	t.Parallel()

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "secret",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"s": "bar"}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse testHarnessResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)

	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
	assert.Equal(t, "**********", predictionResponse.Output)
	assert.Contains(t, predictionResponse.Logs, "reading secret\nwriting secret\n")
}
