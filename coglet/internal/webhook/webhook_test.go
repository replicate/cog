package webhook

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/loggingtest"
)

// Helper function to create a JSON reader from any payload
func jsonReader(t *testing.T, payload any) io.Reader {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return bytes.NewReader(data)
}

func TestNewSender(t *testing.T) {
	t.Parallel()

	logger := loggingtest.NewTestLogger(t)
	sender := NewSender(logger)

	require.NotNil(t, sender)
	assert.Equal(t, logger.Named("webhook"), sender.logger)
}

func TestSenderSend(t *testing.T) {
	t.Parallel()

	t.Run("successful webhook delivery", func(t *testing.T) {
		t.Parallel()

		payload := map[string]any{
			"id":     "test-123",
			"status": "completed",
			"output": "test result",
		}

		var receivedPayload map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)

			err = json.Unmarshal(body, &receivedPayload)
			assert.NoError(t, err)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Serialize payload to bytes and create reader
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		sender := NewSender(loggingtest.NewTestLogger(t))
		err = sender.Send(server.URL, bytes.NewReader(payloadBytes))

		require.NoError(t, err)
		assert.Equal(t, payload, receivedPayload)
	})

	t.Run("handles server errors", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Use a sender with regular HTTP client for faster test
		sender := &DefaultSender{
			logger: loggingtest.NewTestLogger(t).Named("webhook"),
			client: &http.Client{},
		}
		err := sender.Send(server.URL, jsonReader(t, map[string]string{"test": "data"}))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "webhook returned status 500")
	})

	t.Run("handles network errors", func(t *testing.T) {
		t.Parallel()

		// Use a sender with regular HTTP client for faster test
		sender := &DefaultSender{
			logger: loggingtest.NewTestLogger(t).Named("webhook"),
			client: &http.Client{},
		}
		payload := map[string]string{"test": "data"}
		err := sender.Send("http://localhost:99999/webhook", jsonReader(t, payload))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send webhook")
	})

	t.Run("handles invalid URLs", func(t *testing.T) {
		t.Parallel()

		sender := NewSender(loggingtest.NewTestLogger(t))
		err := sender.Send(":", jsonReader(t, map[string]string{"test": "data"}))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create webhook request")
	})
}

func TestSenderSendConditional(t *testing.T) {
	t.Parallel()

	t.Run("sends when no event filter", func(t *testing.T) {
		t.Parallel()

		webhookCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			webhookCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewSender(loggingtest.NewTestLogger(t))
		err := sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventStart, nil, nil)

		require.NoError(t, err)
		assert.True(t, webhookCalled)
	})

	t.Run("sends when event is allowed", func(t *testing.T) {
		t.Parallel()

		webhookCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			webhookCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewSender(loggingtest.NewTestLogger(t))
		allowedEvents := []Event{EventStart, EventCompleted}
		err := sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventStart, allowedEvents, nil)

		require.NoError(t, err)
		assert.True(t, webhookCalled)
	})

	t.Run("skips when event not allowed", func(t *testing.T) {
		t.Parallel()

		webhookCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			webhookCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewSender(loggingtest.NewTestLogger(t))
		allowedEvents := []Event{EventStart, EventCompleted}
		err := sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventLogs, allowedEvents, nil)

		require.NoError(t, err)
		assert.False(t, webhookCalled)
	})

	t.Run("skips when URL is empty", func(t *testing.T) {
		t.Parallel()

		sender := NewSender(loggingtest.NewTestLogger(t))
		err := sender.SendConditional("", jsonReader(t, map[string]string{"test": "data"}), EventStart, nil, nil)

		require.NoError(t, err)
	})

	t.Run("rate limits logs events", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewSender(loggingtest.NewTestLogger(t))
		lastUpdated := time.Now().Add(-time.Second) // Start with old timestamp

		// First call should go through
		err := sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventLogs, nil, &lastUpdated)
		require.NoError(t, err)
		assert.Equal(t, 1, callCount)

		// Immediate second call should be rate limited (lastUpdated was just updated)
		err = sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventLogs, nil, &lastUpdated)
		require.NoError(t, err)
		assert.Equal(t, 1, callCount) // Still 1, not incremented

		// After waiting, should go through
		lastUpdated = time.Now().Add(-time.Second)
		err = sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventLogs, nil, &lastUpdated)
		require.NoError(t, err)
		assert.Equal(t, 2, callCount)
	})

	t.Run("rate limits output events", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewSender(loggingtest.NewTestLogger(t))
		lastUpdated := time.Now().Add(-time.Second) // Start with old timestamp

		// First call should go through
		err := sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventOutput, nil, &lastUpdated)
		require.NoError(t, err)
		assert.Equal(t, 1, callCount)

		// Immediate second call should be rate limited (lastUpdated was just updated)
		err = sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventOutput, nil, &lastUpdated)
		require.NoError(t, err)
		assert.Equal(t, 1, callCount) // Still 1, not incremented
	})

	t.Run("does not rate limit start and completed events", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewSender(loggingtest.NewTestLogger(t))
		lastUpdated := time.Now()

		// Start event should go through
		err := sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventStart, nil, &lastUpdated)
		require.NoError(t, err)
		assert.Equal(t, 1, callCount)

		// Immediate completed event should also go through (no rate limiting)
		err = sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventCompleted, nil, &lastUpdated)
		require.NoError(t, err)
		assert.Equal(t, 2, callCount)
	})

	t.Run("handles nil lastUpdated for rate limiting", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := NewSender(loggingtest.NewTestLogger(t))

		// Should work with nil lastUpdated
		err := sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventLogs, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, 1, callCount)

		// Multiple calls with nil lastUpdated should all go through
		err = sender.SendConditional(server.URL, jsonReader(t, map[string]string{"test": "data"}), EventLogs, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, 2, callCount)
	})
}
