package tests

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/webhook"
)

func TestIteratorTypes(t *testing.T) {
	testCases := []struct {
		module         string
		skipLegacyCog  bool
		maxConcurrency int
	}{
		{
			module: "iterator",
		},
		{
			module:         "async_iterator",
			skipLegacyCog:  true,
			maxConcurrency: 2,
		},
		{
			module: "concat_iterator",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.module, func(t *testing.T) {
			t.Parallel()
			if tc.skipLegacyCog && *legacyCog {
				t.Skipf("skipping %s due to legacy Cog configuration", tc.module)
			}
			runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
				procedureMode:    false,
				explicitShutdown: false,
				uploadURL:        "",
				module:           tc.module,
				predictorClass:   "Predictor",
				concurrencyMax:   tc.maxConcurrency,
			})
			receiverServer := testHarnessReceiverServer(t)

			waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

			input := map[string]any{"i": 2, "s": "bar"}
			req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input, Webhook: receiverServer.URL + "/webhook"})
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusAccepted, resp.StatusCode)
			_, _ = io.Copy(io.Discard, resp.Body)
			var predictionResponse testHarnessResponse
			for webhook := range receiverServer.webhookReceiverChan {
				if webhook.Response.Status == runner.PredictionSucceeded {
					predictionResponse = webhook.Response
					ValidateTerminalResponse(t, &predictionResponse)
					break
				}
			}

			expectedOutput := []any{"*bar-0*", "*bar-1*"}

			assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
			assert.Equal(t, expectedOutput, predictionResponse.Output)
			assert.Equal(t, "starting prediction\nprediction in progress 1/2\nprediction in progress 2/2\ncompleted prediction\n", predictionResponse.Logs)
		})
	}
}

func TestPredictionAsyncIteratorConcurrency(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skipf("skipping async iterator concurrency test due to legacy cog configuration")
	}

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "async_iterator",
		predictorClass:   "Predictor",
		concurrencyMax:   2,
	})
	receiverServer := testHarnessReceiverServer(t)

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	barID, err := runner.PredictionID()
	require.NoError(t, err)
	bazID, err := runner.PredictionID()
	require.NoError(t, err)
	barPrediction := runner.PredictionRequest{
		Input:               map[string]any{"i": 1, "s": "bar"},
		Webhook:             receiverServer.URL + "/webhook",
		ID:                  barID,
		WebhookEventsFilter: []webhook.Event{webhook.EventCompleted},
	}
	bazPrediction := runner.PredictionRequest{
		Input:               map[string]any{"i": 2, "s": "baz"},
		Webhook:             receiverServer.URL + "/webhook",
		ID:                  bazID,
		WebhookEventsFilter: []webhook.Event{webhook.EventCompleted},
	}
	barReq := httpPredictionRequestWithID(t, runtimeServer, barPrediction)
	bazReq := httpPredictionRequestWithID(t, runtimeServer, bazPrediction)
	barResp, err := http.DefaultClient.Do(barReq)
	require.NoError(t, err)
	defer barResp.Body.Close()
	_, _ = io.Copy(io.Discard, barResp.Body)
	bazResp, err := http.DefaultClient.Do(bazReq)
	require.NoError(t, err)
	defer bazResp.Body.Close()
	_, _ = io.Copy(io.Discard, bazResp.Body)
	var barR *testHarnessResponse
	var bazR *testHarnessResponse
	for wh := range receiverServer.webhookReceiverChan {
		assert.Equal(t, runner.PredictionSucceeded, wh.Response.Status)
		switch wh.Response.ID {
		case barPrediction.ID:
			barR = &wh.Response
			ValidateTerminalResponse(t, barR)
		case bazPrediction.ID:
			bazR = &wh.Response
			ValidateTerminalResponse(t, bazR)
		}
		if barR != nil && bazR != nil {
			break
		}
	}
	assert.Equal(t, runner.PredictionSucceeded, barR.Status)
	assert.Equal(t, []any{"*bar-0*"}, barR.Output)
	assert.Equal(t, "starting prediction\nprediction in progress 1/1\ncompleted prediction\n", barR.Logs)
	assert.Equal(t, runner.PredictionSucceeded, bazR.Status)
	assert.Equal(t, []any{"*baz-0*", "*baz-1*"}, bazR.Output)
	assert.Equal(t, "starting prediction\nprediction in progress 1/2\nprediction in progress 2/2\ncompleted prediction\n", bazR.Logs)
}
