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

func TestPredictionDataclassCoderSucceeded(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("legacy Cog does not support custom coder")
	}

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "dataclass",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{
		"account": map[string]any{
			"id":          0,
			"name":        "John",
			"address":     map[string]any{"street": "Smith", "zip": 12345},
			"credentials": map[string]any{"password": "foo", "pubkey": b64encode("bar")},
		},
	}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse runner.PredictionResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)

	expectedOutput := map[string]any{
		"account": map[string]any{
			"id":          100.0,
			"name":        "JOHN",
			"address":     map[string]any{"street": "SMITH", "zip": 22345.0},
			"credentials": map[string]any{"password": "**********", "pubkey": b64encode("*bar*")},
		},
	}
	assert.Equal(t, expectedOutput, predictionResponse.Output)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
}

func TestPredictionChatCoderSucceeded(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("legacy Cog does not support custom coder")
	}

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "chat",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"msg": map[string]any{"role": "assistant", "content": "bar"}}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse runner.PredictionResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)
	expectedOutput := map[string]any{"role": "assistant", "content": "*bar*"}
	assert.Equal(t, expectedOutput, predictionResponse.Output)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
}

func TestPredictionCustomOutputCoder(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("legacy Cog does not support custom coder")
	}

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "custom_output",
		predictorClass:   "Predictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 3}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse runner.PredictionResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)

	// Create expected output using JSON round-trip to match server serialization
	expectedItems := []map[string]any{
		{"x": 3, "y": "a"},
		{"x": 2, "y": "a"},
		{"x": 1, "y": "a"},
	}
	expectedJSON, err := json.Marshal(expectedItems)
	require.NoError(t, err)
	var expectedOutput []any
	err = json.Unmarshal(expectedJSON, &expectedOutput)
	require.NoError(t, err)
	assert.Equal(t, expectedOutput, predictionResponse.Output)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
}

func TestPredictionComplexOutputCoder(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("legacy Cog does not support custom coder")
	}

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "custom_output",
		predictorClass:   "ComplexOutputPredictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 3}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse runner.PredictionResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)

	// Create expected output using JSON round-trip to match server serialization
	expectedComplexOut := map[string]any{
		"a": map[string]any{"x": 3, "y": "a"},
		"b": map[string]any{"x": 3, "y": "b"},
	}
	expectedJSON, err := json.Marshal(expectedComplexOut)
	require.NoError(t, err)
	var expectedOutput any
	err = json.Unmarshal(expectedJSON, &expectedOutput)
	require.NoError(t, err)
	assert.Equal(t, expectedOutput, predictionResponse.Output)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
}

func TestPredictionCustomDataclassOutputCoder(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("legacy Cog does not support custom coder")
	}

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "custom_output",
		predictorClass:   "CustomDataclassOutputPredictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 3}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse runner.PredictionResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)

	// Create expected output using JSON round-trip to match server serialization
	expectedItem := map[string]any{
		"x": 3, "y": "a",
	}
	expectedJSON, err := json.Marshal(expectedItem)
	require.NoError(t, err)
	var expectedOutput any
	err = json.Unmarshal(expectedJSON, &expectedOutput)
	require.NoError(t, err)
	assert.Equal(t, expectedOutput, predictionResponse.Output)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
}

func TestPredictionComplexDataclassOutputCoder(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("legacy Cog does not support custom coder")
	}

	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: true,
		uploadURL:        "",
		module:           "custom_output",
		predictorClass:   "ComplexDataclassOutputPredictor",
	})
	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"i": 3}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var predictionResponse runner.PredictionResponse
	err = json.Unmarshal(body, &predictionResponse)
	require.NoError(t, err)

	// Create expected output using JSON round-trip to match server serialization
	expectedComplexOut := map[string]any{
		"a": map[string]any{"x": 3, "y": "a"},
		"b": map[string]any{"x": 3, "y": "b"},
	}
	expectedJSON, err := json.Marshal(expectedComplexOut)
	require.NoError(t, err)
	var expectedOutput any
	err = json.Unmarshal(expectedJSON, &expectedOutput)
	require.NoError(t, err)
	assert.Equal(t, expectedOutput, predictionResponse.Output)
	assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)
}
