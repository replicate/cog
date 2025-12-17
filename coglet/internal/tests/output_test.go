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

func TestPredictionOutputSucceeded(t *testing.T) {
	t.Parallel()
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "output",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"p": b64encode("bar")}
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
	assert.Contains(t, predictionResponse.Logs, "reading input file\nwriting output file\n")
	var b64 string
	if *legacyCog {
		// Compat: different MIME type detection logic
		b64 = b64encodeLegacy("*bar*")
	} else {
		b64 = b64encode("*bar*")
	}
	expectedOutput := map[string]any{
		"path": b64,
		"text": "*bar*",
	}
	assert.Equal(t, expectedOutput, predictionResponse.Output)
}

func TestComplexOutputTypes(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("legacy Cog does not support complex output types")
	}
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "output_complex_types",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"s": "test"}
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

	// Create expected output using JSON round-trip to match server serialization
	expectedOutputs := []map[string]any{
		{
			"strings": []string{"hello", "world"},
			"numbers": []int{1, 2, 3},
			"single_item": map[string]any{
				"name":  "item1",
				"value": 42,
			},
			"items": []map[string]any{
				{"name": "item1", "value": 42},
				{"name": "item2", "value": 84},
			},
			"container": map[string]any{
				"items": []map[string]any{
					{"name": "item1", "value": 42},
					{"name": "item2", "value": 84},
				},
				"tags": []string{"tag1", "tag2"},
				"nested": map[string]any{
					"item":        map[string]any{"name": "item1", "value": 42},
					"description": "nested description",
				},
				"optional_list": []string{"opt1", "opt2"},
				"count":         2,
			},
			"nested_items": []map[string]any{
				{
					"item":        map[string]any{"name": "item1", "value": 42},
					"description": "nested description",
				},
			},
		},
		{
			"strings": []string{"foo", "bar"},
			"numbers": []int{4, 5, 6},
			"single_item": map[string]any{
				"name":  "item2",
				"value": 84,
			},
			"items": []map[string]any{
				{"name": "item2", "value": 84},
			},
			"container": map[string]any{
				"items": []map[string]any{
					{"name": "item1", "value": 42},
					{"name": "item2", "value": 84},
				},
				"tags": []string{"tag1", "tag2"},
				"nested": map[string]any{
					"item":        map[string]any{"name": "item1", "value": 42},
					"description": "nested description",
				},
				"optional_list": []string{"opt1", "opt2"},
				"count":         2,
			},
			"nested_items": []map[string]any{
				{
					"item":        map[string]any{"name": "item1", "value": 42},
					"description": "nested description",
				},
			},
		},
	}
	expectedJSON, err := json.Marshal(expectedOutputs)
	require.NoError(t, err)
	var expectedOutput []any
	err = json.Unmarshal(expectedJSON, &expectedOutput)
	require.NoError(t, err)
	assert.Equal(t, expectedOutput, predictionResponse.Output)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
}
