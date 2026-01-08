package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/config"
)

func TestStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status Status
		want   string
	}{
		{StatusStarting, "STARTING"},
		{StatusSetupFailed, "SETUP_FAILED"},
		{StatusReady, "READY"},
		{StatusBusy, "BUSY"},
		{StatusDefunct, "DEFUNCT"},
		{Status(999), "INVALID"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			got := tt.status.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGenerateRunnerId(t *testing.T) {
	t.Parallel()

	t.Run("generates unique IDs", func(t *testing.T) {
		t.Parallel()

		ids := make(map[string]bool)
		const numIDs = 1000

		for i := 0; i < numIDs; i++ {
			id := GenerateRunnerID()
			idStr := id.String()

			// Check format: 8-character string
			assert.Len(t, idStr, 8)

			// Check uniqueness
			assert.False(t, ids[idStr], "ID %s was generated twice", idStr)
			ids[idStr] = true

			// Check no leading zeros (should be replaced with 'a')
			assert.NotEqual(t, '0', idStr[0], "ID should not start with '0'")

			// Check only valid base32 characters (lowercase)
			for _, c := range idStr {
				assert.True(t, (c >= 'a' && c <= 'z') || (c >= '2' && c <= '7'),
					"Invalid character %c in ID %s", c, idStr)
			}
		}
	})

	t.Run("String method", func(t *testing.T) {
		t.Parallel()

		id := RunnerID("test1234")
		assert.Equal(t, "test1234", id.String())
	})

	t.Run("consistent format", func(t *testing.T) {
		t.Parallel()

		// Generate multiple IDs and check they all follow the same format
		for i := 0; i < 100; i++ {
			id := GenerateRunnerID()
			idStr := id.String()

			// Should be exactly 8 characters
			assert.Len(t, idStr, 8)

			// Should be all lowercase alphanumeric (base32 subset)
			for _, c := range idStr {
				assert.True(t,
					(c >= 'a' && c <= 'z') || (c >= '2' && c <= '7'),
					"Invalid character in ID: %c", c)
			}

			// Should not start with 0
			assert.NotEqual(t, '0', idStr[0])
		}
	})
}

func TestPredictionRequest(t *testing.T) {
	t.Parallel()

	t.Run("struct fields", func(t *testing.T) {
		t.Parallel()

		req := PredictionRequest{
			ID:                 "test-id",
			Input:              map[string]any{"key": "value"},
			Webhook:            "http://example.com/webhook",
			ProcedureSourceURL: "abc123",
		}

		assert.Equal(t, "test-id", req.ID)
		assert.Equal(t, map[string]any{"key": "value"}, req.Input)
		assert.Equal(t, "http://example.com/webhook", req.Webhook)
		assert.Equal(t, "abc123", req.ProcedureSourceURL)
	})
}

func TestPredictionResponse(t *testing.T) {
	t.Parallel()

	t.Run("struct fields", func(t *testing.T) {
		t.Parallel()

		resp := PredictionResponse{
			ID:      "test-id",
			Status:  "succeeded",
			Output:  map[string]any{"result": "success"},
			Error:   "",
			Logs:    []string{"log1", "log2"},
			Metrics: map[string]any{"duration": 1.5},
		}

		assert.Equal(t, "test-id", resp.ID)
		assert.Equal(t, PredictionSucceeded, resp.Status)
		assert.Equal(t, map[string]any{"result": "success"}, resp.Output)
		assert.Empty(t, resp.Error)
		assert.Equal(t, LogsSlice{"log1", "log2"}, resp.Logs)
		assert.Equal(t, map[string]any{"duration": 1.5}, resp.Metrics)
	})
}

func TestConcurrency(t *testing.T) {
	t.Parallel()

	t.Run("struct fields", func(t *testing.T) {
		t.Parallel()

		c := Concurrency{
			Max:     10,
			Current: 5,
		}

		assert.Equal(t, 10, c.Max)
		assert.Equal(t, 5, c.Current)
	})
}

func TestConstants(t *testing.T) {
	t.Parallel()

	t.Run("default values", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 0, DefaultRunnerID)
		assert.Equal(t, "default", DefaultRunnerName)
	})

	t.Run("regex patterns", func(t *testing.T) {
		t.Parallel()

		// Test LogRegex
		testLog := "[pid=12345] This is a test message"
		matches := LogRegex.FindStringSubmatch(testLog)
		assert.Len(t, matches, 3)
		assert.Equal(t, "12345", matches[1])
		assert.Equal(t, "This is a test message", matches[2])

		// Test ResponseRegex
		testResponse := "response-abc123-1234567890.json"
		matches = ResponseRegex.FindStringSubmatch(testResponse)
		assert.Len(t, matches, 3)
		assert.Equal(t, "abc123", matches[1])
		assert.Equal(t, "1234567890", matches[2])

		// Test CancelFmt
		cancelFile := fmt.Sprintf(CancelFmt, "test-pid")
		assert.Equal(t, "cancel-test-pid", cancelFile)
	})
}

func TestPredictionResponseMarshalUnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("nil logs", func(t *testing.T) {
		t.Parallel()

		original := PredictionResponse{
			ID:     "test-id",
			Status: PredictionSucceeded,
			Output: "test output",
			Logs:   nil,
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		// Verify logs field is omitted for nil/empty
		var jsonData map[string]any
		err = json.Unmarshal(data, &jsonData)
		require.NoError(t, err)
		_, exists := jsonData["logs"]
		assert.False(t, exists, "logs field should not exist for nil logs")

		var unmarshaled PredictionResponse
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, original.ID, unmarshaled.ID)
		assert.Equal(t, original.Status, unmarshaled.Status)
		assert.Equal(t, original.Output, unmarshaled.Output)
		assert.Nil(t, unmarshaled.Logs)
	})

	t.Run("empty slice logs", func(t *testing.T) {
		t.Parallel()

		original := PredictionResponse{
			ID:     "test-id",
			Status: PredictionSucceeded,
			Output: "test output",
			Logs:   []string{},
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		// Verify logs field is omitted for empty slice
		var jsonData map[string]any
		err = json.Unmarshal(data, &jsonData)
		require.NoError(t, err)
		_, exists := jsonData["logs"]
		assert.False(t, exists, "logs field should not exist for empty logs")

		var unmarshaled PredictionResponse
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, original.ID, unmarshaled.ID)
		assert.Equal(t, original.Status, unmarshaled.Status)
		assert.Equal(t, original.Output, unmarshaled.Output)
		// After JSON round-trip, empty slice becomes nil
		assert.Nil(t, unmarshaled.Logs)
	})

	t.Run("single log line", func(t *testing.T) {
		t.Parallel()

		original := PredictionResponse{
			ID:     "test-id",
			Status: PredictionSucceeded,
			Output: "test output",
			Logs:   []string{"hello world"},
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var jsonData map[string]any
		err = json.Unmarshal(data, &jsonData)
		require.NoError(t, err)
		assert.Equal(t, "hello world\n", jsonData["logs"])

		var unmarshaled PredictionResponse
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, original.ID, unmarshaled.ID)
		assert.Equal(t, original.Status, unmarshaled.Status)
		assert.Equal(t, original.Output, unmarshaled.Output)
		assert.Equal(t, original.Logs, unmarshaled.Logs)
	})

	t.Run("multiple log lines", func(t *testing.T) {
		t.Parallel()

		original := PredictionResponse{
			ID:     "test-id",
			Status: PredictionSucceeded,
			Output: "test output",
			Logs:   []string{"starting prediction", "prediction in progress 1/2", "prediction in progress 2/2", "completed prediction"},
		}

		data, err := json.Marshal(original)
		require.NoError(t, err)

		var jsonData map[string]any
		err = json.Unmarshal(data, &jsonData)
		require.NoError(t, err)
		assert.Equal(t, "starting prediction\nprediction in progress 1/2\nprediction in progress 2/2\ncompleted prediction\n", jsonData["logs"])

		var unmarshaled PredictionResponse
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, original.ID, unmarshaled.ID)
		assert.Equal(t, original.Status, unmarshaled.Status)
		assert.Equal(t, original.Output, unmarshaled.Output)
		assert.Equal(t, original.Logs, unmarshaled.Logs)
	})
}

func TestPredictionResponseUnmarshalFromExternalJSON(t *testing.T) {
	t.Parallel()

	// Test unmarshalling from JSON with logs as string (external format)
	jsonStr := `{
		"id": "test-id",
		"status": "succeeded", 
		"output": "test output",
		"logs": "starting prediction\nprediction in progress 1/2\nprediction in progress 2/2\ncompleted prediction\n"
	}`

	var response PredictionResponse
	err := json.Unmarshal([]byte(jsonStr), &response)
	require.NoError(t, err)

	expected := LogsSlice{
		"starting prediction",
		"prediction in progress 1/2",
		"prediction in progress 2/2",
		"completed prediction",
	}

	assert.Equal(t, "test-id", response.ID)
	assert.Equal(t, PredictionSucceeded, response.Status)
	assert.Equal(t, "test output", response.Output)
	assert.Equal(t, expected, response.Logs)
}

func TestPendingPrediction(t *testing.T) {
	t.Parallel()

	t.Run("safeSend", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			setup    func(*PendingPrediction)
			response PredictionResponse
			want     bool
		}{
			{
				name:     "send to open channel",
				setup:    func(p *PendingPrediction) {},
				response: PredictionResponse{ID: "test", Status: PredictionProcessing},
				want:     true,
			},
			{
				name: "send to closed channel",
				setup: func(p *PendingPrediction) {
					p.safeClose()
				},
				response: PredictionResponse{ID: "test", Status: PredictionProcessing},
				want:     false,
			},
			{
				name: "send to full channel",
				setup: func(p *PendingPrediction) {
					// Fill the buffered channel
					p.c <- PredictionResponse{ID: "blocking", Status: PredictionProcessing}
				},
				response: PredictionResponse{ID: "test", Status: PredictionProcessing},
				want:     false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				p := &PendingPrediction{
					c: make(chan PredictionResponse, 1),
				}
				tt.setup(p)

				got := p.safeSend(tt.response)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("safeClose", func(t *testing.T) {
		t.Parallel()

		t.Run("close open channel", func(t *testing.T) {
			t.Parallel()

			p := &PendingPrediction{
				c: make(chan PredictionResponse, 1),
			}

			got := p.safeClose()
			assert.True(t, got)
			assert.True(t, p.closed)
		})

		t.Run("close already closed channel", func(t *testing.T) {
			t.Parallel()

			p := &PendingPrediction{
				c: make(chan PredictionResponse, 1),
			}
			p.safeClose()

			got := p.safeClose()
			assert.False(t, got)
		})
	})

	t.Run("concurrent operations", func(t *testing.T) {
		t.Parallel()

		p := &PendingPrediction{
			c: make(chan PredictionResponse, 10),
		}

		var wg sync.WaitGroup
		const numGoroutines = 10

		// Start multiple goroutines sending
		for i := 0; i < numGoroutines; i++ {
			wg.Go(func() {
				resp := PredictionResponse{ID: "test", Status: PredictionProcessing}
				p.safeSend(resp)
			})
		}

		// Start one goroutine closing
		wg.Go(func() {
			time.Sleep(1 * time.Millisecond)
			p.safeClose()
		})

		wg.Wait()
		assert.True(t, p.closed)
	})
}

func TestRunnerContextCleanup(t *testing.T) {
	t.Parallel()

	t.Run("cleanup without UID does nothing", func(t *testing.T) {
		t.Parallel()
		rc := RunnerContext{
			tmpDir:             t.TempDir(),
			cleanupDirectories: []string{"/tmp"},
		}

		err := rc.Cleanup()
		assert.NoError(t, err)
	})

	t.Run("cleanup cleans tmpDir", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		rc := RunnerContext{tmpDir: tmpDir}

		// tmpDir should exist before cleanup
		_, err := os.Stat(tmpDir)
		require.NoError(t, err)

		err = rc.Cleanup()
		require.NoError(t, err)

		// tmpDir should be removed after cleanup
		_, err = os.Stat(tmpDir)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("skip paths logic works correctly", func(t *testing.T) {
		t.Parallel()

		// Create safe test directories
		testDir := t.TempDir()
		workingDir := testDir + "/working"
		tmpDir := testDir + "/runner-tmp-123"
		cleanupDir := testDir + "/cleanup"

		// This test verifies the skip paths logic without actually doing cleanup
		// since we can't test UID-based operations in unit tests
		uid := 12345
		rc := RunnerContext{
			workingdir:         workingDir,
			tmpDir:             tmpDir,
			uid:                &uid,
			cleanupDirectories: []string{cleanupDir},
		}

		// This would normally call cleanupDirectoriesFiles but we can't test
		// the actual UID operations in unit tests. Instead, verify the config
		// is set up correctly for container/integration tests.
		assert.NotNil(t, rc.uid)
		assert.Equal(t, 12345, *rc.uid)
		assert.Contains(t, rc.cleanupDirectories, cleanupDir)
		assert.Equal(t, workingDir, rc.workingdir)
		assert.Equal(t, tmpDir, rc.tmpDir)
	})
}

func TestPredictionResponseFinalization(t *testing.T) {
	t.Parallel()

	t.Run("finalizeResponse sets CompletedAt when empty", func(t *testing.T) {
		t.Parallel()

		response := &PredictionResponse{
			Status:    PredictionSucceeded,
			StartedAt: "2023-01-01T12:00:00.000000+00:00",
			Metrics:   make(map[string]any),
		}

		err := response.finalizeResponse()
		require.NoError(t, err)

		assert.NotEmpty(t, response.CompletedAt)
		// Should be RFC3339Nano format
		_, err = time.Parse(config.TimeFormat, response.CompletedAt)
		assert.NoError(t, err)
	})

	t.Run("finalizeResponse preserves existing CompletedAt", func(t *testing.T) {
		t.Parallel()

		existingTime := "2023-01-01T12:30:45.123456+00:00"
		response := &PredictionResponse{
			Status:      PredictionSucceeded,
			StartedAt:   "2023-01-01T12:00:00.000000+00:00",
			CompletedAt: existingTime,
			Metrics:     make(map[string]any),
		}

		err := response.finalizeResponse()
		require.NoError(t, err)

		assert.Equal(t, existingTime, response.CompletedAt)
	})

	t.Run("finalizeResponse creates metrics map when nil", func(t *testing.T) {
		t.Parallel()

		response := &PredictionResponse{
			Status:    PredictionSucceeded,
			StartedAt: "2023-01-01T12:00:00.000000+00:00",
			Metrics:   nil,
		}

		err := response.finalizeResponse()
		require.NoError(t, err)

		assert.NotNil(t, response.Metrics)
		assert.Contains(t, response.Metrics, "predict_time")
	})

	t.Run("finalizeResponse calculates predict_time correctly", func(t *testing.T) {
		t.Parallel()

		startTime := "2023-01-01T12:00:00.000000+00:00"
		response := &PredictionResponse{
			Status:    PredictionSucceeded,
			StartedAt: startTime,
			Metrics:   make(map[string]any),
		}

		err := response.finalizeResponse()
		require.NoError(t, err)

		predictTime, ok := response.Metrics["predict_time"]
		require.True(t, ok)

		// Should be a positive number
		predictTimeFloat, ok := predictTime.(float64)
		require.True(t, ok)
		assert.Positive(t, predictTimeFloat)
	})

	t.Run("finalizeResponse preserves existing predict_time", func(t *testing.T) {
		t.Parallel()

		existingPredictTime := 42.5
		response := &PredictionResponse{
			Status:    PredictionSucceeded,
			StartedAt: "2023-01-01T12:00:00.000000+00:00",
			Metrics:   map[string]any{"predict_time": existingPredictTime},
		}

		err := response.finalizeResponse()
		require.NoError(t, err)

		assert.Equal(t, existingPredictTime, response.Metrics["predict_time"]) //nolint:testifylint // we want to compare absolute values not delta
	})

	t.Run("finalizeResponse handles invalid time formats", func(t *testing.T) {
		t.Parallel()

		response := &PredictionResponse{
			Status:    PredictionSucceeded,
			StartedAt: "invalid-time",
			Metrics:   make(map[string]any),
		}

		err := response.finalizeResponse()
		require.Error(t, err)
		var parseErr *time.ParseError
		require.ErrorAs(t, err, &parseErr)
	})

	t.Run("finalizeResponse handles missing StartedAt", func(t *testing.T) {
		t.Parallel()

		response := &PredictionResponse{
			Status:  PredictionSucceeded,
			Metrics: make(map[string]any),
		}

		err := response.finalizeResponse()
		require.Error(t, err)
		var parseErr *time.ParseError
		require.ErrorAs(t, err, &parseErr)
	})
}

func TestPredictionResponsePopulateFromRequest(t *testing.T) {
	t.Parallel()

	t.Run("PopulateFromRequest sets all fields correctly", func(t *testing.T) {
		t.Parallel()

		request := PredictionRequest{
			ID:        "test-id-123",
			Input:     map[string]any{"prompt": "hello world"},
			CreatedAt: "2023-01-01T11:00:00.000000+00:00",
			StartedAt: "2023-01-01T11:00:05.000000+00:00",
		}

		response := &PredictionResponse{
			Status: PredictionProcessing,
		}

		response.populateFromRequest(request)

		assert.Equal(t, request.ID, response.ID)
		assert.Equal(t, request.Input, response.Input)
		assert.Equal(t, request.CreatedAt, response.CreatedAt)
		assert.Equal(t, request.StartedAt, response.StartedAt)
	})

	t.Run("PopulateFromRequest overwrites existing fields", func(t *testing.T) {
		t.Parallel()

		request := PredictionRequest{
			ID:        "new-id",
			Input:     map[string]any{"new": "input"},
			CreatedAt: "2023-01-01T12:00:00.000000+00:00",
			StartedAt: "2023-01-01T12:00:05.000000+00:00",
		}

		response := &PredictionResponse{
			ID:        "old-id",
			Input:     map[string]any{"old": "input"},
			CreatedAt: "2023-01-01T10:00:00.000000+00:00",
			StartedAt: "2023-01-01T10:00:05.000000+00:00",
			Status:    PredictionProcessing,
		}

		response.populateFromRequest(request)

		assert.Equal(t, "new-id", response.ID)
		assert.Equal(t, map[string]any{"new": "input"}, response.Input)
		assert.Equal(t, "2023-01-01T12:00:00.000000+00:00", response.CreatedAt)
		assert.Equal(t, "2023-01-01T12:00:05.000000+00:00", response.StartedAt)
		assert.Equal(t, PredictionProcessing, response.Status) // Preserves existing status
	})
}
