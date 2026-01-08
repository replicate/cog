package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/coglet/internal/runner"
)

func TestInputFunctionSchemaGeneration(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("Input generation tests coglet specific implementations.")
	}
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "input_function",
		predictorClass:   "Predictor",
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	resp, err := http.Get(runtimeServer.URL + "/openapi.json")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var schema map[string]any
	err = json.Unmarshal(body, &schema)
	require.NoError(t, err)

	assert.Contains(t, schema, "components")

	components := schema["components"].(map[string]any)
	assert.Contains(t, components, "schemas")

	schemas := components["schemas"].(map[string]any)
	assert.Contains(t, schemas, "Input")

	inputSchema := schemas["Input"].(map[string]any)
	assert.Equal(t, "object", inputSchema["type"])
	assert.Contains(t, inputSchema, "properties")
	assert.Contains(t, inputSchema, "required")

	properties := inputSchema["properties"].(map[string]any)
	required := inputSchema["required"].([]any)

	assert.Contains(t, properties, "message")
	assert.Contains(t, required, "message")
	messageField := properties["message"].(map[string]any)
	assert.Equal(t, "string", messageField["type"])
	assert.Equal(t, "Message to process", messageField["description"])

	assert.Contains(t, properties, "repeat_count")
	assert.NotContains(t, required, "repeat_count")
	repeatField := properties["repeat_count"].(map[string]any)
	assert.Equal(t, "integer", repeatField["type"])
	assert.Equal(t, float64(1), repeatField["default"])  //nolint:testifylint // Checking absolute value not delta
	assert.Equal(t, float64(1), repeatField["minimum"])  //nolint:testifylint // Checking absolute value not delta
	assert.Equal(t, float64(10), repeatField["maximum"]) //nolint:testifylint // Checking absolute value not delta

	assert.Contains(t, properties, "prefix")
	prefixField := properties["prefix"].(map[string]any)
	assert.Equal(t, "string", prefixField["type"])
	assert.Equal(t, "Result: ", prefixField["default"])
	assert.Equal(t, float64(1), prefixField["minLength"])  //nolint:testifylint // Checking absolute value not delta
	assert.Equal(t, float64(20), prefixField["maxLength"]) //nolint:testifylint // Checking absolute value not delta

	assert.Contains(t, properties, "deprecated_option")
	deprecatedField := properties["deprecated_option"].(map[string]any)
	assert.Equal(t, true, deprecatedField["deprecated"])
}

func TestInputFunctionBasicPrediction(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("Input generation tests coglet specific implementations.")
	}
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "input_function",
		predictorClass:   "Predictor",
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"message": "hello world"}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var prediction runner.PredictionResponse
	err = json.Unmarshal(body, &prediction)
	require.NoError(t, err)

	assert.Equal(t, runner.PredictionSucceeded, prediction.Status)
	assert.Equal(t, "Result: hello world", prediction.Output)
}

func TestInputFunctionComplexPrediction(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("Input generation tests coglet specific implementations.")
	}
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "input_function",
		predictorClass:   "Predictor",
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{
		"message":           "test message",
		"repeat_count":      2,
		"format_type":       "uppercase",
		"prefix":            "Output: ",
		"suffix":            " [END]",
		"deprecated_option": "custom",
	}
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
	ValidateTerminalResponse(t, &prediction)
	assert.Equal(t, "Output: TEST MESSAGE TEST MESSAGE [END]", prediction.Output)
}

func TestInputFunctionConstraintViolations(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("Input generation tests coglet specific implementations.")
	}
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "input_function",
		predictorClass:   "Predictor",
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	testCases := []struct {
		name     string
		input    map[string]any
		errorMsg string
	}{
		{
			name:     "repeat_count too low",
			input:    map[string]any{"message": "test", "repeat_count": 0},
			errorMsg: "fails constraint >= 1",
		},
		{
			name:     "repeat_count too high",
			input:    map[string]any{"message": "test", "repeat_count": 11},
			errorMsg: "fails constraint <= 10",
		},
		{
			name:     "invalid format_type choice",
			input:    map[string]any{"message": "test", "format_type": "invalid"},
			errorMsg: "does not match choices",
		},
		{
			name:     "prefix too short",
			input:    map[string]any{"message": "test", "prefix": ""},
			errorMsg: "fails constraint len() >= 1",
		},
		{
			name:     "prefix too long",
			input:    map[string]any{"message": "test", "prefix": strings.Repeat("x", 21)},
			errorMsg: "fails constraint len() <= 20",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: tc.input})

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var errorResp testHarnessResponse
			t.Logf("body: %s", string(body))
			err = json.Unmarshal(body, &errorResp)
			require.NoError(t, err)

			assert.Equal(t, runner.PredictionFailed, errorResp.Status)
			ValidateTerminalResponse(t, &errorResp)
			assert.Contains(t, errorResp.Error, tc.errorMsg)
			// FIXME: python's internal task for sending IPC updates has a 100ms delay
			// without adding a delay here now that go is a lot more async, we will
			// fail the prediction since we have not reset from `BUSY` to `READY`
			waitForReady(t, runtimeServer)
		})
	}
}

func TestInputFunctionMissingRequired(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("Input generation tests coglet specific implementations.")
	}
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "input_function",
		predictorClass:   "Predictor",
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"repeat_count": 2}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var errorResp runner.PredictionResponse
	err = json.Unmarshal(body, &errorResp)
	require.NoError(t, err)

	assert.Equal(t, runner.PredictionFailed, errorResp.Status)
	assert.Contains(t, errorResp.Error, "missing required input field: message")
}

func TestInputFunctionSimple(t *testing.T) {
	t.Parallel()
	if *legacyCog {
		t.Skip("Input generation tests coglet specific implementations.")
	}
	runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
		procedureMode:    false,
		explicitShutdown: false,
		uploadURL:        "",
		module:           "input_simple",
		predictorClass:   "Predictor",
	})

	waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

	input := map[string]any{"message": "hello", "count": 3}
	req := httpPredictionRequest(t, runtimeServer, runner.PredictionRequest{Input: input})

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var prediction runner.PredictionResponse
	err = json.Unmarshal(body, &prediction)
	require.NoError(t, err)

	assert.Equal(t, runner.PredictionSucceeded, prediction.Status)
	assert.Equal(t, "hellohellohello", prediction.Output)
}
