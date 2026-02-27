package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockParser is a test parser that returns a fixed PredictorInfo.
func mockParser(source []byte, predictRef string, mode Mode) (*PredictorInfo, error) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("prompt", InputField{
		Name:      "prompt",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Required},
	})
	p := TypeString
	return &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   mode,
	}, nil
}

// failParser always returns an error.
func failParser(_ []byte, _ string, _ Mode) (*PredictorInfo, error) {
	return nil, NewError(ErrParse, "mock parse failure")
}

// ---------------------------------------------------------------------------
// parsePredictRef
// ---------------------------------------------------------------------------

func TestParsePredictRef(t *testing.T) {
	tests := []struct {
		input   string
		file    string
		name    string
		wantErr bool
	}{
		{"predict.py:Predictor", "predict.py", "Predictor", false},
		{"src/model.py:MyModel", "src/model.py", "MyModel", false},
		{"train.py:train", "train.py", "train", false},
		{"no_colon", "", "", true},
		{":NoFile", "", "", true},
		{"no_name:", "", "", true},
		{"", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			file, name, err := parsePredictRef(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				var se *SchemaError
				require.ErrorAs(t, err, &se)
				assert.Equal(t, ErrInvalidPredictRef, se.Kind)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.file, file)
				assert.Equal(t, tt.name, name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GenerateFromSource
// ---------------------------------------------------------------------------

func TestGenerateFromSource(t *testing.T) {
	data, err := GenerateFromSource([]byte("unused"), "Predictor", ModePredict, mockParser)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, json.Unmarshal(data, &spec))

	assert.Equal(t, "3.0.2", spec["openapi"])
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	assert.Contains(t, props, "prompt")
}

func TestGenerateFromSourceTrainMode(t *testing.T) {
	data, err := GenerateFromSource([]byte("unused"), "Trainer", ModeTrain, mockParser)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, json.Unmarshal(data, &spec))

	assert.NotNil(t, getPath(spec, "components", "schemas", "TrainingInput"))
	assert.NotNil(t, getPath(spec, "paths", "/trainings", "post"))
}

func TestGenerateFromSourceParseError(t *testing.T) {
	_, err := GenerateFromSource([]byte("unused"), "Predictor", ModePredict, failParser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mock parse failure")
}

// ---------------------------------------------------------------------------
// Generate — file-based
// ---------------------------------------------------------------------------

func TestGenerateReadsFile(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "predict.py"), []byte("class Predictor: pass"), 0o644)
	require.NoError(t, err)

	data, err := Generate("predict.py:Predictor", dir, ModePredict, mockParser)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, json.Unmarshal(data, &spec))
	assert.Equal(t, "3.0.2", spec["openapi"])
}

func TestGenerateMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Generate("missing.py:Predictor", dir, ModePredict, mockParser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read predictor source")
}

func TestGenerateInvalidRef(t *testing.T) {
	_, err := Generate("no_colon", ".", ModePredict, mockParser)
	require.Error(t, err)
	var se *SchemaError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrInvalidPredictRef, se.Kind)
}

// ---------------------------------------------------------------------------
// COG_OPENAPI_SCHEMA env var
// ---------------------------------------------------------------------------

func TestGenerateCogOpenAPISchemaEnv(t *testing.T) {
	// Write a pre-built schema file
	dir := t.TempDir()
	schemaContent := `{"openapi": "3.0.2", "info": {"title": "Custom"}}`
	schemaPath := filepath.Join(dir, "custom_schema.json")
	err := os.WriteFile(schemaPath, []byte(schemaContent), 0o644)
	require.NoError(t, err)

	t.Setenv("COG_OPENAPI_SCHEMA", schemaPath)

	// Should return the custom schema without parsing
	// (using failParser to prove parsing is skipped)
	data, err := Generate("predict.py:Predictor", ".", ModePredict, failParser)
	require.NoError(t, err)
	assert.Equal(t, schemaContent, string(data))
}

func TestGenerateCogOpenAPISchemaEnvMissingFile(t *testing.T) {
	t.Setenv("COG_OPENAPI_SCHEMA", "/nonexistent/schema.json")

	_, err := Generate("predict.py:Predictor", ".", ModePredict, mockParser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COG_OPENAPI_SCHEMA")
	assert.Contains(t, err.Error(), "failed to read")
}

func TestGenerateCogOpenAPISchemaEnvNotSet(t *testing.T) {
	// Ensure env var is not set
	t.Setenv("COG_OPENAPI_SCHEMA", "")

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "predict.py"), []byte("class Predictor: pass"), 0o644)
	require.NoError(t, err)

	// Should proceed with normal generation (not use env var)
	data, err := Generate("predict.py:Predictor", dir, ModePredict, mockParser)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, json.Unmarshal(data, &spec))
	assert.Equal(t, "Cog", getPath(spec, "info", "title"))
}
