package predict

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestValidateInputMapForMode(t *testing.T) {
	schema := validationTestSchema(t)

	tests := []struct {
		name    string
		input   map[string]any
		wantErr string
	}{
		{
			name: "valid input",
			input: map[string]any{
				"prompt": "hello",
				"steps":  5.0,
				"mode":   "fast",
				"opts":   map[string]any{"scale": 2.0},
			},
		},
		{
			name:    "unknown key",
			input:   map[string]any{"prompt": "hello", "typo": true},
			wantErr: "unknown input \"typo\"",
		},
		{
			name:    "missing required",
			input:   map[string]any{"steps": 5.0},
			wantErr: "missing required input \"prompt\"",
		},
		{
			name:    "wrong type",
			input:   map[string]any{"prompt": 10.0},
			wantErr: "invalid input \"prompt\": expected string, got number",
		},
		{
			name:    "numeric constraint",
			input:   map[string]any{"prompt": "hello", "steps": 0.0},
			wantErr: "invalid input \"steps\"",
		},
		{
			name:    "enum ref",
			input:   map[string]any{"prompt": "hello", "mode": "medium"},
			wantErr: "invalid input \"mode\"",
		},
		{
			name:    "object field",
			input:   map[string]any{"prompt": "hello", "opts": map[string]any{"scale": "bad"}},
			wantErr: "invalid input \"opts.scale\": expected number, got string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInputMapForMode(tt.input, schema, false)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateInputsForModeUsesCoercedValues(t *testing.T) {
	schema := validationTestSchema(t)
	inputs := Inputs{
		"prompt": {String: strPtr("hello")},
		"steps":  {Int: intPtr(5)},
	}
	require.NoError(t, ValidateInputsForMode(inputs, schema, false))
}

func TestValidateInputsForModeDecodesRawJSON(t *testing.T) {
	schema := validationTestSchema(t)
	raw := json.RawMessage(`{"scale": 2}`)
	inputs := Inputs{
		"prompt": {String: strPtr("hello")},
		"opts":   {Json: &raw},
	}
	require.NoError(t, ValidateInputsForMode(inputs, schema, false))
}

func TestValidateInputsForModeValidatesNamesBeforeFileReads(t *testing.T) {
	schema := validationTestSchema(t)
	missingPath := "/no/such/file"
	inputs := Inputs{
		"typo": {File: &missingPath},
	}
	err := ValidateInputsForMode(inputs, schema, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown input \"typo\"")
	require.NotContains(t, err.Error(), missingPath)
}

func TestValidateInputMapForModeNoInputs(t *testing.T) {
	schema := validationTestSchema(t)
	schema.Components.Schemas["Input"].Value.Properties = openapi3.Schemas{}
	err := ValidateInputMapForMode(map[string]any{"typo": "hello"}, schema, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown input \"typo\"; model does not accept inputs")
}

func TestValidateInputMapForModeTrainingInput(t *testing.T) {
	schema := validationTestSchema(t)
	require.NoError(t, ValidateInputMapForMode(map[string]any{"epochs": 1.0}, schema, true))
	err := ValidateInputMapForMode(map[string]any{"prompt": "hello"}, schema, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown input \"prompt\"")
}

func TestValidateInputMapForModeTrainingFallbackToInput(t *testing.T) {
	schema := validationTestSchema(t)
	delete(schema.Components.Schemas, "TrainingInput")
	require.NoError(t, ValidateInputMapForMode(map[string]any{"prompt": "hello"}, schema, true))
}

func TestValidateInputMapForModeMultipleUnknown(t *testing.T) {
	schema := validationTestSchema(t)
	err := ValidateInputMapForMode(map[string]any{"a": 1, "b": 2}, schema, false)
	require.EqualError(t, err, `unknown inputs "a", "b"; valid inputs are: flag, mode, nums, opts, prompt, steps`)
}

func TestValidateInputMapForModeMultipleUnknownNoInputs(t *testing.T) {
	schema := validationTestSchema(t)
	schema.Components.Schemas["Input"].Value.Properties = openapi3.Schemas{}
	err := ValidateInputMapForMode(map[string]any{"a": 1, "b": 2}, schema, false)
	require.EqualError(t, err, `unknown inputs "a", "b"; model does not accept inputs`)
}

func TestValidateInputMapForModeMultipleErrors(t *testing.T) {
	schema := validationTestSchema(t)
	err := ValidateInputMapForMode(map[string]any{"prompt": 10.0, "steps": 0.0}, schema, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "input validation failed:")
	require.Contains(t, err.Error(), `invalid input "prompt": expected string, got number`)
	require.Contains(t, err.Error(), `invalid input "steps": number must be at least 1`)
}

func TestValidateInputMapForModeNilSchema(t *testing.T) {
	require.EqualError(t, ValidateInputMapForMode(map[string]any{}, nil, false),
		"OpenAPI schema is required to validate inputs")
}

func TestValidateInputMapForModeMissingComponents(t *testing.T) {
	require.EqualError(t, ValidateInputMapForMode(map[string]any{}, &openapi3.T{}, false),
		"OpenAPI schema is missing components")
}

func TestInputComponentForModeTrainingAndInputMissing(t *testing.T) {
	schema := validationTestSchema(t)
	delete(schema.Components.Schemas, "TrainingInput")
	delete(schema.Components.Schemas, "Input")
	require.EqualError(t, ValidateInputNamesForMode(map[string]any{"x": 1}, schema, true),
		"OpenAPI schema is missing TrainingInput or Input component")
	require.EqualError(t, ValidateInputNamesForMode(map[string]any{"x": 1}, schema, false),
		"OpenAPI schema is missing Input component")
}

// TestValidationErrorMessagesAreStable pins the exact user-facing wording so a
// kin-openapi upgrade that changes the library's internal reason strings (which
// we string-match and pass through) is caught instead of silently degrading.
func TestValidationErrorMessagesAreStable(t *testing.T) {
	schema := validationTestSchema(t)
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"missing required", map[string]any{"steps": 5.0}, `missing required input "prompt"`},
		{"wrong type", map[string]any{"prompt": 10.0}, `invalid input "prompt": expected string, got number`},
		{"nested type", map[string]any{"prompt": "hi", "opts": map[string]any{"scale": "bad"}}, `invalid input "opts.scale": expected number, got string`},
		{"enum via allOf", map[string]any{"prompt": "hi", "mode": "medium"}, `invalid input "mode": must be one of: fast, slow`},
		{"minimum", map[string]any{"prompt": "hi", "steps": 0.0}, `invalid input "steps": number must be at least 1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.EqualError(t, ValidateInputMapForMode(tc.input, schema, false), tc.want)
		})
	}
}

func TestFormatExpectedTypes(t *testing.T) {
	require.Equal(t, "valid JSON value", formatExpectedTypes(nil))
	require.Equal(t, "string", formatExpectedTypes([]string{"string"}))
	require.Equal(t, "string or integer", formatExpectedTypes([]string{"string", "integer"}))
	require.Equal(t, "string, integer or boolean", formatExpectedTypes([]string{"string", "integer", "boolean"}))
}

func TestJSONTypeName(t *testing.T) {
	require.Equal(t, "null", jsonTypeName(nil))
	require.Equal(t, "boolean", jsonTypeName(true))
	require.Equal(t, "number", jsonTypeName(3.14))
	require.Equal(t, "number", jsonTypeName(int32(5)))
	require.Equal(t, "string", jsonTypeName("x"))
	require.Equal(t, "array", jsonTypeName([]any{1}))
	require.Equal(t, "object", jsonTypeName(map[string]any{"a": 1}))
}

func validationTestSchema(t *testing.T) *openapi3.T {
	t.Helper()
	data := []byte(`{
  "openapi": "3.0.2",
  "info": {"title": "Cog", "version": "test"},
  "paths": {},
  "components": {
    "schemas": {
      "Mode": {"type": "string", "enum": ["fast", "slow"]},
      "Input": {
        "type": "object",
        "required": ["prompt"],
        "properties": {
          "prompt": {"type": "string", "minLength": 1},
          "steps": {"type": "integer", "minimum": 1, "maximum": 10},
          "flag": {"type": "boolean"},
          "nums": {"type": "array", "items": {"type": "integer"}},
          "mode": {"allOf": [{"$ref": "#/components/schemas/Mode"}]},
          "opts": {
            "type": "object",
            "properties": {"scale": {"type": "number"}}
          }
        }
      },
      "TrainingInput": {
        "type": "object",
        "required": ["epochs"],
        "properties": {"epochs": {"type": "integer", "minimum": 1}}
      }
    }
  }
}`)
	loader := openapi3.NewLoader()
	schema, err := loader.LoadFromData(data)
	require.NoError(t, err)
	return schema
}

func strPtr(s string) *string {
	return &s
}

func intPtr(i int32) *int32 {
	return &i
}
