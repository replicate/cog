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

func TestPathOut(t *testing.T) {
	validB64Data := []string{
		b64encodeLegacy("*foo*"),
		b64encode("*foo*"),
	}

	testCases := []struct {
		predictor     string
		nested        bool
		skipLegacyCog bool
	}{
		// Output type is Path
		{
			predictor:     "path_out",
			nested:        false,
			skipLegacyCog: false,
		},
		// Output type is Any
		{
			predictor: "path_out_any",
			nested:    true,
		},
		// Output type is dataclass
		{
			predictor:     "path_out_dataclass",
			nested:        true,
			skipLegacyCog: true,
		},
		// Output type is dict[str, Any]
		{
			predictor:     "path_out_json",
			nested:        true,
			skipLegacyCog: false,
		},
		// Output type is cog.Output
		{
			predictor:     "path_out_output",
			nested:        true,
			skipLegacyCog: false,
		},
		// Output type is os.PathLike
		{
			predictor:     "path_out_pathlike",
			nested:        false,
			skipLegacyCog: true,
		},
		// Output type is a Pydantic base model
		{
			predictor:     "path_out_pydantic",
			nested:        true,
			skipLegacyCog: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.predictor, func(t *testing.T) {
			t.Parallel()
			if testCase.skipLegacyCog && *legacyCog {
				t.Skipf("skipping %s due to legacy Cog configuration", testCase.predictor)
			}
			runtimeServer := setupCogRuntime(t, cogRuntimeServerConfig{
				procedureMode:    false,
				explicitShutdown: false,
				uploadURL:        "",
				module:           testCase.predictor,
				predictorClass:   "Predictor",
			})
			waitForSetupComplete(t, runtimeServer, runner.StatusReady, runner.SetupSucceeded)

			prediction := runner.PredictionRequest{Input: map[string]any{"s": "foo"}}
			req := httpPredictionRequest(t, runtimeServer, prediction)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			var predictionResponse runner.PredictionResponse
			err = json.Unmarshal(body, &predictionResponse)
			require.NoError(t, err)
			assert.Equal(t, runner.PredictionSucceeded, predictionResponse.Status)

			if testCase.nested {
				assert.Len(t, predictionResponse.Output, 1)
				outputRaw, exists := predictionResponse.Output.(map[string]any)
				require.True(t, exists, "output is not a map[string]any")
				output, ok := outputRaw["p"].(string)
				require.True(t, ok, "output is not a string")
				assert.Contains(t, validB64Data, output)
			} else {
				output, ok := predictionResponse.Output.(string)
				require.True(t, ok, "output is not a string")
				assert.Contains(t, validB64Data, output)
			}
		})
	}
}
