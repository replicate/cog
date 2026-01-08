package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/replicate/cog/coglet/internal/runner"
	"github.com/replicate/cog/coglet/internal/webhook"
)

// TestProcedureSchemaLoadingSequential tests that schema loading works correctly
// for sequential predictions in procedure mode where runners may be cleaned up
// between predictions. This is a specific regression test for the issue where
// schema loading timing caused problems after runner recreation.
func TestProcedureSchemaLoadingSequential(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("procedure endpoint has diverged from legacy Cog")
	}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-jpeg-data-for-testing"))
	}))
	t.Cleanup(testServer.Close)

	receiverServer := testHarnessReceiverServer(t)

	runtimeServer, _, _ := setupCogRuntimeServer(t, cogRuntimeServerConfig{
		procedureMode:    true,
		explicitShutdown: true,
		uploadURL:        "",
		maxRunners:       1, // Force runner recreation between predictions
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)
	procedureURL := fmt.Sprintf("file://%s/python/tests/procedures/path_test", basePath)

	// Run 3 sequential predictions to test schema loading robustness
	for i := 0; i < 3; i++ {
		prediction := runner.PredictionRequest{
			Input: map[string]any{
				"img": fmt.Sprintf("%s/image-%d.jpg", testServer.URL, i),
			},
			Context: map[string]any{
				"procedure_source_url": procedureURL,
				"replicate_api_token":  "test-token",
			},
			Webhook:             receiverServer.URL + "/webhook",
			WebhookEventsFilter: []webhook.Event{webhook.EventCompleted},
		}

		_, statusCode := runProcedure(t, runtimeServer, prediction)
		assert.Equal(t, http.StatusAccepted, statusCode)

		var wh webhookData
		select {
		case wh = <-receiverServer.webhookReceiverChan:
		case <-time.After(10 * time.Second):
			t.Errorf("timeout waiting for webhook for prediction %d", i)
			return
		}

		assert.Equal(t, runner.PredictionSucceeded, wh.Response.Status, "prediction %d should succeed", i)

		// Verify URL processing worked - this is the key regression test
		output, ok := wh.Response.Output.(string)
		assert.True(t, ok, "output should be a string for prediction %d", i)
		assert.True(t, strings.HasPrefix(output, "data:"),
			"prediction %d: HTTPS URL should be downloaded and converted to base64, got: %s", i, output)
	}
}
