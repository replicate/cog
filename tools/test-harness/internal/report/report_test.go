package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRound(t *testing.T) {
	tests := []struct {
		val       float64
		precision int
		want      float64
	}{
		{1.555, 2, 1.56},
		{1.554, 2, 1.55},
		{-1.555, 2, -1.56},
		{-1.554, 2, -1.55},
		{0.0, 2, 0.0},
		{123.456, 0, 123.0},
		{123.556, 0, 124.0},
		{1.5, 0, 2.0},
		{-1.5, 0, -2.0},
	}

	for _, tt := range tests {
		got := round(tt.val, tt.precision)
		assert.InDelta(t, tt.want, got, 0.001, "round(%f, %d) = %f, want %f", tt.val, tt.precision, got, tt.want)
	}
}

func TestJSONReport(t *testing.T) {
	results := []ModelResult{
		{
			Name:          "model-a",
			Passed:        true,
			BuildDuration: 10.123,
			TestResults: []TestResult{
				{Description: "basic predict", Passed: true, Message: "ok", DurationSec: 2.5},
			},
		},
		{
			Name:    "model-b",
			Passed:  false,
			Error:   "build failed",
			Skipped: false,
			GPU:     true,
		},
		{
			Name:       "model-c",
			Passed:     true,
			Skipped:    true,
			SkipReason: "Missing env var: API_KEY",
		},
	}

	report := JSONReport(results, "0.17.0", "v0.17.2")

	// Check summary
	summary, ok := report["summary"].(map[string]int)
	require.True(t, ok, "summary should be a map[string]int")
	assert.Equal(t, 3, summary["total"])
	assert.Equal(t, 1, summary["passed"])
	assert.Equal(t, 1, summary["failed"])
	assert.Equal(t, 1, summary["skipped"])

	// Check versions
	assert.Equal(t, "0.17.0", report["sdk_version"])
	assert.Equal(t, "v0.17.2", report["cog_version"])

	// Check models
	models, ok := report["models"].([]map[string]any)
	require.True(t, ok, "models should be a slice")
	require.Len(t, models, 3)

	// model-a: passed with tests
	assert.Equal(t, "model-a", models[0]["name"])
	assert.Equal(t, true, models[0]["passed"])
	assert.Equal(t, 10.12, models[0]["build_duration_s"])
	tests, ok := models[0]["tests"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, tests, 1)
	assert.Equal(t, "basic predict", tests[0]["description"])

	// model-b: failed with error
	assert.Equal(t, "model-b", models[1]["name"])
	assert.Equal(t, false, models[1]["passed"])
	assert.Equal(t, "build failed", models[1]["error"])
	assert.Equal(t, true, models[1]["gpu"])

	// model-c: skipped
	assert.Equal(t, "model-c", models[2]["name"])
	assert.Equal(t, true, models[2]["skipped"])
	assert.Equal(t, "Missing env var: API_KEY", models[2]["skip_reason"])
}

func TestWriteJSONReport(t *testing.T) {
	results := []ModelResult{
		{Name: "test-model", Passed: true, BuildDuration: 5.0},
	}

	var buf bytes.Buffer
	err := WriteJSONReport(results, "0.17.0", "v0.17.2", &buf)
	require.NoError(t, err)

	// Verify it's valid JSON
	var parsed map[string]any
	err = json.Unmarshal(buf.Bytes(), &parsed)
	require.NoError(t, err)

	assert.Contains(t, parsed, "timestamp")
	assert.Contains(t, parsed, "models")
	assert.Contains(t, parsed, "summary")
}

func TestJSONReportEmptyResults(t *testing.T) {
	report := JSONReport(nil, "", "")

	summary, ok := report["summary"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 0, summary["total"])
	assert.Equal(t, 0, summary["passed"])
	assert.Equal(t, 0, summary["failed"])
	assert.Equal(t, 0, summary["skipped"])

	models, ok := report["models"].([]map[string]any)
	require.True(t, ok)
	assert.Empty(t, models)
}

func TestSchemaCompareJSONReport(t *testing.T) {
	results := []SchemaCompareResult{
		{Name: "model-a", Passed: true, StaticBuild: 5.0, RuntimeBuild: 8.0},
		{Name: "model-b", Passed: false, Error: "build failed"},
	}

	report := SchemaCompareJSONReport(results, "v0.17.2")

	summary, ok := report["summary"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 2, summary["total"])
	assert.Equal(t, 1, summary["passed"])
	assert.Equal(t, 1, summary["failed"])
}
