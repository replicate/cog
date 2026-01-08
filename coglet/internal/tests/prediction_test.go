package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/webhook"
)

func TestPredictionSucceeded(t *testing.T) {
	t.Parallel()
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "sleep",
		predictorClass:   "Predictor",
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 1, "s": "bar"}
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
	assert.Equal(t, "*bar*", prediction.Output)
	assert.Contains(t, prediction.Logs, "starting prediction\nprediction in progress 1/1\ncompleted prediction\n")
	assert.Equal(t, 1.0, prediction.Metrics["i"])     //nolint:testifylint // verifying absolute equality
	assert.Equal(t, 3.0, prediction.Metrics["s_len"]) //nolint:testifylint // verifying absolute equality
}

func TestPredictionWithIdSucceeded(t *testing.T) {
	t.Parallel()
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "sleep",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 1, "s": "bar"}
	predictionID, err := runner.PredictionID()
	require.NoError(t, err)
	predictionReq := runner.PredictionRequest{
		ID:    predictionID,
		Input: input,
	}
	req := httpPredictionRequestWithID(t, runtimeServer, predictionReq)

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
	assert.Equal(t, "*bar*", predictionResponse.Output)
	assert.Equal(t, predictionID, predictionResponse.ID)
	assert.Contains(t, predictionResponse.Logs, "starting prediction\nprediction in progress 1/1\ncompleted prediction\n")
}

func TestPredictionFailure(t *testing.T) {
	t.Parallel()
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "sleep",
		predictorClass:   "PredictionFailingPredictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 1, "s": "bar"}
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

	assert.Equal(t, runner.PredictionFailed, predictionResponse.Status)
	assert.Nil(t, predictionResponse.Output)
	assert.Contains(t, predictionResponse.Logs, "starting prediction\nprediction failed\n")
	assert.Equal(t, "prediction failed", predictionResponse.Error)
}

func TestPredictionCrash(t *testing.T) {
	t.Parallel()

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "sleep",
		predictorClass:   "PredictionCrashingPredictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 1, "s": "bar"}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	hc := healthCheck(t, runtimeServer)
	switch resp.StatusCode {
	case http.StatusInternalServerError:
		// This is "legacy" cog semantics
		assert.Equal(t, "Internal Server Error", string(body))
		assert.Equal(t, "DEFUNCT", hc.Status)
	case http.StatusOK:
		require.NoError(t, err)
		var predictionResponse testHarnessResponse
		err = json.Unmarshal(body, &predictionResponse)
		require.NoError(t, err)
		assert.Equal(t, runner.PredictionFailed, predictionResponse.Status)
		assert.Nil(t, predictionResponse.Output)
		assert.Contains(t, predictionResponse.Logs, "starting prediction")
		assert.Contains(t, predictionResponse.Logs, "SystemExit: 1\n")
		assert.Equal(t, "prediction failed", predictionResponse.Error)
		assert.Equal(t, "DEFUNCT", hc.Status)
	default:
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
}

func TestPredictionConcurrency(t *testing.T) {
	t.Parallel()

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "sleep",
		predictorClass:   "Predictor",
	})
	receiverServer := testHarnessReceiverServer(t)

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 5, "s": "bar"}

	firstPredictionSent := make(chan bool, 1)

	wg := sync.WaitGroup{}

	wg.Go(func() {
		predictionReq := runner.PredictionRequest{
			Input:               input,
			Webhook:             receiverServer.URL + "/webhook",
			WebhookEventsFilter: []webhook.Event{webhook.EventCompleted},
		}
		req := httpPredictionRequest(t, runtimeServer, predictionReq)
		resp, err := http.DefaultClient.Do(req)
		close(firstPredictionSent)
		require.NoError(t, err)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)
		var webhook webhookData
		select {
		case webhook = <-receiverServer.webhookReceiverChan:
		case <-time.After(10 * time.Second):
			assert.Fail(t, "timeout waiting for webhook")
		}
		assert.Equal(t, runner.PredictionSucceeded, webhook.Response.Status)
		// NOTE(morgan): since we're using the webhook format, the deserialization
		// of `i` is a float64, so we need to convert it to an int, since we've already
		// shipped the input, we can change it directly
		expectedInput := input
		expectedInput["i"] = float64(5)
		assert.Equal(t, expectedInput, webhook.Response.Input)
		assert.Equal(t, "*bar*", webhook.Response.Output)
		assert.Contains(t, webhook.Response.Logs, "starting prediction\nprediction in progress 1/5\nprediction in progress 2/5\nprediction in progress 3/5\nprediction in progress 4/5\nprediction in progress 5/5\ncompleted prediction\n")
	})

	predictionReq := runner.PredictionRequest{
		Input: input,
	}
	req := httpPredictionRequest(t, runtimeServer, predictionReq)
	t.Log("waiting for first prediction to be sent")
	select {
	case <-firstPredictionSent:
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for first prediction to be sent")
	}
	t.Log("first prediction sent, attempting second prediction send")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	// Ensure the first prediction is completed
	wg.Wait()
}
