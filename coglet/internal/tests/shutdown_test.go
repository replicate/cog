package tests

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/webhook"
)

func TestShutdownEndpointE2E(t *testing.T) {
	if *legacyCog {
		t.Skip("Not relevant for legacy cog, testing internal behavior of the go server")
	}
	// Use the proper test harness
	httpTestServer, _, svc := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:             false,
		explicitShutdown:          true,
		uploadURL:                 "",
		module:                    "async_sleep",
		predictorClass:            "Predictor",
		concurrencyMax:            1,
		runnerShutdownGracePeriod: 100 * time.Millisecond,
	})
	defer httpTestServer.Close()

	// Wait for setup to complete
	waitForSetupComplete(t, httpTestServer, runner.StatusReady, runner.SetupSucceeded)

	baseURL := httpTestServer.URL

	// Verify service is responding before testing shutdown
	healthResp, err := http.Get(baseURL + "/health-check")
	require.NoError(t, err)
	defer healthResp.Body.Close()
	require.Equal(t, http.StatusOK, healthResp.StatusCode)

	// Test the /shutdown endpoint
	shutdownResp, err := http.Post(baseURL+"/shutdown", "application/json", nil)
	require.NoError(t, err)
	defer shutdownResp.Body.Close()

	// Should return 200 OK
	assert.Equal(t, http.StatusOK, shutdownResp.StatusCode)

	// Service should shutdown gracefully
	require.Eventually(t, func() bool {
		return svc.IsStopped()
	}, 1*time.Second, 10*time.Millisecond, "service should have stopped after shutdown")
	// Service should no longer be running
	assert.False(t, svc.IsRunning())
	assert.True(t, svc.IsStopped())
}

func TestShutdownEndpointWaitsForInflightPredictions(t *testing.T) {
	if *legacyCog {
		t.Skip("Not relevant for legacy cog, testing internal behavior of the go server")
	}
	// Set up webhook receiver using test harness helper
	receiverServer := testHarnessReceiverServer(t)

	// Use the proper test harness to get handler, service, and httptest server
	httpTestServer, _, svc := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:             false,
		explicitShutdown:          true,
		uploadURL:                 receiverServer.URL + "/upload/",
		module:                    "async_sleep", // Use async predictor for cancellation support
		predictorClass:            "Predictor",
		concurrencyMax:            1,
		runnerShutdownGracePeriod: 200 * time.Millisecond, // Allow time for graceful shutdown
	})
	defer httpTestServer.Close()

	// Wait for setup to complete
	waitForSetupComplete(t, httpTestServer, runner.StatusReady, runner.SetupSucceeded)

	// Use the httptest server URL
	baseURL := httpTestServer.URL

	// Start an async prediction
	predictionID, err := runner.PredictionID()
	require.NoError(t, err)

	prediction := runner.PredictionRequest{
		Input:   map[string]any{"i": 30, "s": "test"}, // 30 second sleep but we'll cancel it
		Webhook: receiverServer.URL + "/webhook",
		WebhookEventsFilter: []webhook.Event{
			webhook.EventStart,
			webhook.EventCompleted,
		},
		ID: predictionID,
	}

	req := httpPredictionRequestWithID(t, httpTestServer, prediction)
	predResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer predResp.Body.Close()
	require.Equal(t, http.StatusAccepted, predResp.StatusCode)

	// Wait for webhook to confirm prediction started
	select {
	case webhookEvent := <-receiverServer.webhookReceiverChan:
		assert.Equal(t, runner.PredictionProcessing, webhookEvent.Response.Status)
		assert.Equal(t, predictionID, webhookEvent.Response.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive prediction started webhook")
	}

	// Now trigger shutdown while prediction is running via HTTP endpoint
	shutdownResp, err := http.Post(baseURL+"/shutdown", "application/json", nil)
	require.NoError(t, err)
	defer shutdownResp.Body.Close()
	assert.Equal(t, http.StatusOK, shutdownResp.StatusCode)

	// Verify new predictions are rejected during shutdown with 503
	newPredictionID, err := runner.PredictionID()
	require.NoError(t, err)
	newPrediction := runner.PredictionRequest{
		Input:   map[string]any{"i": 1, "s": "should_be_rejected"},
		Webhook: receiverServer.URL + "/webhook",
		ID:      newPredictionID,
	}
	newReq := httpPredictionRequestWithID(t, httpTestServer, newPrediction)
	newResp, err := http.DefaultClient.Do(newReq)
	require.NoError(t, err)
	defer newResp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, newResp.StatusCode, "new predictions should be rejected with 503 during shutdown")

	// Cancel the running prediction to allow graceful completion
	cancelResp, err := http.Post(baseURL+"/predictions/"+predictionID+"/cancel", "application/json", nil)
	require.NoError(t, err)
	defer cancelResp.Body.Close()
	assert.Equal(t, http.StatusOK, cancelResp.StatusCode)

	// Wait for final webhook (canceled)
	select {
	case webhookEvent := <-receiverServer.webhookReceiverChan:
		assert.Equal(t, predictionID, webhookEvent.Response.ID)
		assert.Equal(t, runner.PredictionCanceled, webhookEvent.Response.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive prediction canceled webhook")
	}

	// The key test: verify the SERVICE itself has shut down gracefully
	// Wait for service to stop (it should stop automatically after shutdown)
	require.Eventually(t, func() bool {
		return svc.IsStopped()
	}, 10*time.Second, 10*time.Millisecond, "service should have stopped after shutdown")

	// Service should no longer be running
	assert.False(t, svc.IsRunning())
	assert.True(t, svc.IsStopped())
}

func TestShutdownEndpointFailsInNonAwaitMode(t *testing.T) {
	if *legacyCog {
		t.Skip("Not relevant for legacy cog, testing internal behavior of the go server")
	}
	// Use the test harness but with non-await mode
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false, // This is the key difference - no await explicit shutdown
		uploadURL:        "",
		module:           "async_sleep",
		predictorClass:   "Predictor",
	})
	defer runtimeServer.Close()

	// Wait for setup to complete
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	baseURL := runtimeServer.URL

	// Test the /shutdown endpoint - should still work even in non-await mode
	// The difference is that in non-await mode, the service doesn't wait for SIGTERM
	shutdownResp, err := http.Post(baseURL+"/shutdown", "application/json", nil)
	require.NoError(t, err)
	defer shutdownResp.Body.Close()

	// Should still return 200 OK
	assert.Equal(t, http.StatusOK, shutdownResp.StatusCode)

	// Give it a moment for shutdown to process
	time.Sleep(50 * time.Millisecond)

	// Verify server has shut down by attempting health check
	// Should fail after shutdown completes since the server should be down
	require.Eventually(t, func() bool {
		resp, err := http.Get(baseURL + "/health-check")
		if resp != nil {
			resp.Body.Close()
		}
		return err != nil // Server should be down
	}, 2*time.Second, 50*time.Millisecond, "server should have shut down")
}

func TestShutdownEndpointMultipleCallsIdempotent(t *testing.T) {
	if *legacyCog {
		t.Skip("Not relevant for legacy cog, testing internal behavior of the go server")
	}
	// Use the proper test harness
	httpTestServer, _, svc := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "async_sleep",
		predictorClass:   "Predictor",
		concurrencyMax:   1,
	})
	defer httpTestServer.Close()

	// Wait for setup to complete
	waitForSetupComplete(t, httpTestServer, runner.StatusReady, runner.SetupSucceeded)

	baseURL := httpTestServer.URL

	// Call shutdown multiple times rapidly
	for i := range 3 {
		shutdownResp, err := http.Post(baseURL+"/shutdown", "application/json", nil)
		if err != nil {
			// After first shutdown, subsequent requests may fail due to server being down
			// This is expected behavior
			break
		}
		shutdownResp.Body.Close()
		// First call should succeed, subsequent calls may or may not depending on timing
		if i == 0 {
			assert.Equal(t, http.StatusOK, shutdownResp.StatusCode)
		}
	}

	// Service should shutdown gracefully (only once)
	require.Eventually(t, func() bool {
		return svc.IsStopped()
	}, 1*time.Second, 10*time.Millisecond, "service should have stopped after shutdown")

	assert.False(t, svc.IsRunning())
	assert.True(t, svc.IsStopped())
}
