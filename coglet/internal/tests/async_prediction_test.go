package tests

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/webhook"
)

func TestAsyncPrediction(t *testing.T) {
	testCases := []struct {
		name             string
		predictorClass   string
		expectedLogs     string
		expectedOutput   any
		expectedStatus   runner.PredictionStatus
		expectedHCStatus string
	}{
		{
			name:           "succeeded",
			predictorClass: "Predictor",
			expectedLogs:   "starting prediction\nprediction in progress 1/1\ncompleted prediction\n",
			expectedOutput: "*bar*",
			expectedStatus: runner.PredictionSucceeded,
		},
		{
			name:           "failed",
			predictorClass: "PredictionFailingPredictor",
			expectedLogs:   "starting prediction\nprediction failed\n",
			expectedOutput: nil,
			expectedStatus: runner.PredictionFailed,
		},

		{
			name:             "crashed",
			predictorClass:   "PredictionCrashingPredictor",
			expectedLogs:     "starting prediction\nprediction crashed\n",
			expectedOutput:   nil,
			expectedStatus:   runner.PredictionFailed,
			expectedHCStatus: runner.StatusDefunct.String(),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			receiverServer := testHarnessReceiverServer(t)
			runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
				procedureMode:    false,
				explicitShutdown: true,
				uploadURL:        "",
				module:           "sleep",
				predictorClass:   tc.predictorClass,
				concurrencyMax:   1,
			})
			waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

			predictionID, err := runner.PredictionID()
			require.NoError(t, err)
			prediction := runner.PredictionRequest{
				Input:   map[string]any{"i": 1, "s": "bar"},
				Webhook: receiverServer.URL + "/webhook",
				WebhookEventsFilter: []webhook.Event{
					webhook.EventCompleted,
				},
				ID: predictionID,
			}
			req := httpPredictionRequestWithID(t, runtimeServer, prediction)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusAccepted, resp.StatusCode)
			_, _ = io.Copy(io.Discard, resp.Body)

			// Validate the result via the webhook message
			select {
			case webhookEvent := <-receiverServer.webhookReceiverChan:
				assert.Equal(t, tc.expectedStatus, webhookEvent.Response.Status)
				assert.Contains(t, webhookEvent.Response.Logs, tc.expectedLogs)
				assert.Equal(t, tc.expectedOutput, webhookEvent.Response.Output)
			case <-time.After(10 * time.Second):
				t.Fatalf("timeout waiting for webhook")
			}

			if tc.expectedHCStatus != "" {
				hc := healthCheck(t, runtimeServer)
				assert.Equal(t, tc.expectedHCStatus, hc.Status)
			}
		})
	}
}

func TestAsyncPredictionCanceled(t *testing.T) {
	t.Parallel()
	// FIXME: This is a case where `file_runner.py` has a sync/async mismatch. Even though execution context is yielded back to the async runner,
	// if we're in a blocking I/O (or many other cases) the async cancellation will never propagate to the predictor (and the predictor would need to
	// explicitly handle the cancellation). The previous test crashed the predictor so that cancellation could work as there was nothing actually
	// running or blocking the cancellation.The only way to fix this without drastically changing how we run python (e.g. keeping `file_runner.py` as
	// is) would be to abandon the thread that is running the non-async predictor which would cause further orphaning of processes. For the most
	// part, we'll just swallow the cancellation request and continue to process the prediction (similar to how TRTLLM worked under cog).
	t.Skipf("FIXME: Due to a mismatch how file_runner.py handles sync python with an async runner, it is impossible to cancel a sync python predictor without crashing the prediction before trying to cancel it.")
	receiverServer := testHarnessReceiverServer(t)
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "sleep",
		predictorClass:   "Predictor",
		concurrencyMax:   2,
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	predictionID, err := runner.PredictionID()
	require.NoError(t, err)
	prediction := runner.PredictionRequest{
		Input:   map[string]any{"i": 60, "s": "bar"},
		Webhook: receiverServer.URL + "/webhook",
		ID:      predictionID,
		WebhookEventsFilter: []webhook.Event{
			webhook.EventStart,
			webhook.EventLogs,
			webhook.EventCompleted,
		},
	}
	req := httpPredictionRequestWithID(t, runtimeServer, prediction)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	_, _ = io.Copy(io.Discard, resp.Body)

	// Wait for a single wh, then continue on.
	var wh webhookData
	select {
	case <-receiverServer.webhookReceiverChan:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for initial webhook")
	}

	cancelReq, err := http.NewRequest(http.MethodPost, runtimeServer.URL+fmt.Sprintf("/predictions/%s/cancel", predictionID), nil)
	require.NoError(t, err)
	cancelResp, err := http.DefaultClient.Do(cancelReq)
	require.NoError(t, err)
	defer cancelResp.Body.Close()
	assert.Equal(t, http.StatusOK, cancelResp.StatusCode)
	_, _ = io.Copy(io.Discard, cancelResp.Body)

	// Find the "prediction canceled" webhook, we could get any number of webhooks before this.
waitLoop:
	for {
		select {
		case wh = <-receiverServer.webhookReceiverChan:
			if wh.Response.Status != runner.PredictionProcessing {
				// We only break out if we get a prediction canceled webhook. without the
				// named loop we can only break out of the select case.
				break waitLoop
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("timeout waiting for webhook")
		}
	}

	assert.Equal(t, runner.PredictionCanceled, wh.Response.Status)
	assert.Equal(t, predictionID, wh.Response.ID)
	// NOTE(morgan): The logs are not deterministic, so we can only assert that `prediction canceled` is in the logs.
	// previously we asserted that the prediction was making progress. We are assured that we have a "starting" webhook, but
	// internally this test not reacts faster than the runner does.
	assert.Contains(t, wh.Response.Logs, "prediction canceled\n")
}

func TestAsyncPredictionConcurrency(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skipf("HealthCheck concurrency is not implemented in legacy Cog")
	}
	receiverServer := testHarnessReceiverServer(t)
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "sleep",
		predictorClass:   "Predictor",
		// FIXME: The doesn't really affect the values in the healthcheck, those are hard-coded to 1 for non-procedure mode.
		concurrencyMax: 1,
	})
	hc := waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)
	assert.Equal(t, 1, hc.Concurrency.Max)
	assert.Equal(t, 0, hc.Concurrency.Current)

	predictionID, err := runner.PredictionID()
	require.NoError(t, err)
	prediction := runner.PredictionRequest{
		Input:   map[string]any{"i": 1, "s": "bar"},
		Webhook: receiverServer.URL + "/webhook",
		ID:      predictionID,
	}
	req := httpPredictionRequestWithID(t, runtimeServer, prediction)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	_, _ = io.Copy(io.Discard, resp.Body)

	// Show that concurrency has
	hc = healthCheck(t, runtimeServer)
	assert.Equal(t, 1, hc.Concurrency.Max)
	assert.Equal(t, 1, hc.Concurrency.Current)
}
