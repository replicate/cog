package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/webhook"
)

// TestPathInputHTTPSRegression tests that HTTPS URLs are properly downloaded
// and converted to local paths across all runtime modes. This prevents regression
// of the issue where schema loading timing caused URLs to be passed unchanged.
func TestPathInputHTTPSRegression(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("this test is validating the cog-runtime server's behavior not legacy cog")
	}
	// Create a test server that serves dummy image data
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-jpeg-data-for-testing"))
	}))
	t.Cleanup(testServer.Close)

	testCases := []struct {
		name          string
		procedureMode bool
		useWebhook    bool
	}{
		{"procedure-mode-no-webhook", true, false},
		{"procedure-mode-with-webhook", true, true},
		{"non-procedure-mode-no-webhook", false, false},
		{"non-procedure-mode-with-webhook", false, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if *legacyCog && tc.procedureMode {
				t.Skip("procedure endpoint has diverged from legacy Cog")
			}

			var receiverServer *testHarnessReceiver
			if tc.useWebhook {
				receiverServer = testHarnessReceiverServer(t)
			}

			// Setup runtime server
			if tc.procedureMode {
				testProcedureModeHTTPSProcessing(t, testServer, receiverServer)
			} else {
				testNonProcedureModeHTTPSProcessing(t, testServer, receiverServer)
			}
		})
	}
}

func testProcedureModeHTTPSProcessing(t *testing.T, testServer *httptest.Server, receiverServer *testHarnessReceiver) {
	t.Helper()
	runtimeServer, _, _ := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:    true,
		explicitShutdown: true,
		uploadURL:        "",
		maxRunners:       1,
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	procedureURL := fmt.Sprintf("file://%s/python/tests/procedures/path_test", basePath)

	prediction := runner.PredictionRequest{
		Input: map[string]any{
			"img": testServer.URL + "/test-image.jpg",
		},
		Context: map[string]any{
			"procedure_source_url": procedureURL,
			"replicate_api_token":  "test-token",
		},
	}

	if receiverServer != nil {
		prediction.Webhook = receiverServer.URL + "/webhook"
		prediction.WebhookEventsFilter = []webhook.Event{webhook.EventCompleted}

		predictionID, statusCode := runProcedure(t, runtimeServer, prediction)
		require.Equal(t, http.StatusAccepted, statusCode)

		// Wait for webhook completion
		var wh webhookData
		select {
		case wh = <-receiverServer.webhookReceiverChan:
		case <-time.After(30 * time.Second):
			t.Fatal("timeout waiting for webhook")
		}

		assert.Equal(t, runner.PredictionSucceeded, wh.Response.Status)
		assert.Equal(t, predictionID, wh.Response.ID)

		// Key assertion: URL should be processed into base64 data URL
		output, ok := wh.Response.Output.(string)
		require.True(t, ok, "output should be a string")
		assert.True(t, strings.HasPrefix(output, "data:"),
			"HTTPS URL should be downloaded and converted to base64 data URL, got: %s", output)
		assert.NotEqual(t, testServer.URL+"/test-image.jpg", output,
			"output should not be the original HTTPS URL - this means URL processing failed")
	} else {
		// For procedure mode without webhook, make direct HTTP request
		req := httpPredictionRequest(t, runtimeServer, prediction)
		req.URL.Path = "/procedures"
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get synchronous response with the result
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var predictionResponse runner.PredictionResponse
		err = json.Unmarshal(body, &predictionResponse)
		require.NoError(t, err)

		assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)

		// Key assertion: URL should be processed into base64 data URL
		output, ok := predictionResponse.Output.(string)
		require.True(t, ok, "output should be a string")
		assert.True(t, strings.HasPrefix(output, "data:"),
			"HTTPS URL should be downloaded and converted to base64 data URL, got: %s", output)
		assert.NotEqual(t, testServer.URL+"/test-image.jpg", output,
			"output should not be the original HTTPS URL - this means URL processing failed")
	}
}

func testNonProcedureModeHTTPSProcessing(t *testing.T, testServer *httptest.Server, receiverServer *testHarnessReceiver) {
	t.Helper()
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "path_input_https",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	prediction := runner.PredictionRequest{
		Input: map[string]any{
			"img": testServer.URL + "/test-image.jpg",
		},
	}

	if receiverServer != nil {
		prediction.Webhook = receiverServer.URL + "/webhook"
		prediction.WebhookEventsFilter = []webhook.Event{webhook.EventCompleted}
	}

	if receiverServer != nil {
		// Async mode with webhook
		req := httpPredictionRequest(t, runtimeServer, prediction)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

		// Wait for webhook completion
		var wh webhookData
		select {
		case wh = <-receiverServer.webhookReceiverChan:
		case <-time.After(30 * time.Second):
			t.Fatal("timeout waiting for webhook")
		}

		assert.Equal(t, runner.PredictionSucceeded, wh.Response.Status)

		// Key assertion: URL should be processed into base64 data URL
		output, ok := wh.Response.Output.(string)
		require.True(t, ok, "output should be a string")
		assert.True(t, strings.HasPrefix(output, "data:"),
			"HTTPS URL should be downloaded and converted to base64 data URL, got: %s", output)
		assert.NotEqual(t, testServer.URL+"/test-image.jpg", output,
			"output should not be the original HTTPS URL - this means URL processing failed")
	} else {
		// Synchronous mode without webhook
		req := httpPredictionRequest(t, runtimeServer, prediction)
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

		// Key assertion: URL should be processed into base64 data URL
		output, ok := predictionResponse.Output.(string)
		require.True(t, ok, "output should be a string")
		assert.True(t, strings.HasPrefix(output, "data:"),
			"HTTPS URL should be downloaded and converted to base64 data URL, got: %s", output)
		assert.NotEqual(t, testServer.URL+"/test-image.jpg", output,
			"output should not be the original HTTPS URL - this means URL processing failed")
	}
}
