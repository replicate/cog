package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func simplePredictor() *PredictorInfo {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("s", InputField{
		Name:      "s",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Required},
	})

	p := TypeString
	return &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}
}

func ptrStr(s string) *string     { return &s }
func ptrFloat(f float64) *float64 { return &f }
func ptrUint(u uint64) *uint64    { return &u }
func ptrBool(b bool) *bool        { return &b }

// parseSpec is a test helper that generates the schema and unmarshals
// it into a generic map for assertion.
func parseSpec(t *testing.T, info *PredictorInfo) map[string]any {
	t.Helper()
	data, err := GenerateOpenAPISchema(info)
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, json.Unmarshal(data, &spec))
	return spec
}

func getPath(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[k]
	}
	return cur
}

// ---------------------------------------------------------------------------
// Tests: Top-level structure
// ---------------------------------------------------------------------------

func TestGeneratesValidOpenAPI(t *testing.T) {
	spec := parseSpec(t, simplePredictor())

	assert.Equal(t, "3.0.2", spec["openapi"])
	assert.Equal(t, "Cog", getPath(spec, "info", "title"))
	assert.Equal(t, "0.1.0", getPath(spec, "info", "version"))
}

func TestPredictEndpoints(t *testing.T) {
	spec := parseSpec(t, simplePredictor())

	// Root
	assert.NotNil(t, getPath(spec, "paths", "/", "get"))
	// Health check
	assert.NotNil(t, getPath(spec, "paths", "/health-check", "get"))
	// Predictions
	post := getPath(spec, "paths", "/predictions", "post")
	require.NotNil(t, post)
	postMap := post.(map[string]any)
	assert.Equal(t, "Predict", postMap["summary"])
	assert.Equal(t, "predict_predictions_post", postMap["operationId"])
	// Cancel
	assert.NotNil(t, getPath(spec, "paths", "/predictions/{prediction_id}/cancel", "post"))
}

func TestTrainEndpoints(t *testing.T) {
	info := simplePredictor()
	info.Mode = ModeTrain
	spec := parseSpec(t, info)

	post := getPath(spec, "paths", "/trainings", "post")
	require.NotNil(t, post)
	postMap := post.(map[string]any)
	assert.Equal(t, "Train", postMap["summary"])
	assert.Equal(t, "train_trainings_post", postMap["operationId"])

	// Cancel
	cancel := getPath(spec, "paths", "/trainings/{training_id}/cancel", "post")
	require.NotNil(t, cancel)

	// Schema keys use TrainingInput/TrainingOutput
	assert.NotNil(t, getPath(spec, "components", "schemas", "TrainingInput"))
	assert.NotNil(t, getPath(spec, "components", "schemas", "TrainingOutput"))
	assert.NotNil(t, getPath(spec, "components", "schemas", "TrainingRequest"))
	assert.NotNil(t, getPath(spec, "components", "schemas", "TrainingResponse"))
}

// ---------------------------------------------------------------------------
// Tests: Fixed components
// ---------------------------------------------------------------------------

func TestFixedComponentSchemas(t *testing.T) {
	spec := parseSpec(t, simplePredictor())
	schemas := getPath(spec, "components", "schemas").(map[string]any)

	// PredictionRequest
	req := schemas["PredictionRequest"].(map[string]any)
	assert.Equal(t, "PredictionRequest", req["title"])
	props := req["properties"].(map[string]any)
	assert.Equal(t, "#/components/schemas/Input", getPath(props, "input", "$ref"))
	assert.Equal(t, "string", getPath(props, "id", "type"))

	// PredictionResponse
	resp := schemas["PredictionResponse"].(map[string]any)
	assert.Equal(t, "PredictionResponse", resp["title"])
	respProps := resp["properties"].(map[string]any)
	assert.Equal(t, "#/components/schemas/Input", getPath(respProps, "input", "$ref"))
	assert.Equal(t, "#/components/schemas/Output", getPath(respProps, "output", "$ref"))

	// Status
	status := schemas["Status"].(map[string]any)
	assert.Equal(t, "string", status["type"])
	enum := status["enum"].([]any)
	assert.Contains(t, enum, "starting")
	assert.Contains(t, enum, "succeeded")

	// Validation errors
	assert.NotNil(t, schemas["HTTPValidationError"])
	assert.NotNil(t, schemas["ValidationError"])
}

// ---------------------------------------------------------------------------
// Tests: Input schema
// ---------------------------------------------------------------------------

func TestInputRequiredField(t *testing.T) {
	spec := parseSpec(t, simplePredictor())
	input := getPath(spec, "components", "schemas", "Input").(map[string]any)

	required := input["required"].([]any)
	assert.Contains(t, required, "s")
}

func TestInputOptionalFieldNotRequired(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("name", InputField{
		Name:      "name",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Optional},
		Default:   &DefaultValue{Kind: DefaultNone},
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	input := getPath(spec, "components", "schemas", "Input").(map[string]any)

	// Should not have required since there's a default
	assert.Nil(t, input["required"])

	// Should have nullable
	props := input["properties"].(map[string]any)
	nameField := props["name"].(map[string]any)
	assert.Equal(t, true, nameField["nullable"])
}

func TestInputDefaultValue(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("count", InputField{
		Name:      "count",
		Order:     0,
		FieldType: FieldType{Primitive: TypeInteger, Repetition: Required},
		Default:   &DefaultValue{Kind: DefaultInt, Int: 42},
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	countField := props["count"].(map[string]any)
	// JSON numbers unmarshal as float64
	assert.Equal(t, float64(42), countField["default"])
}

func TestInputDescription(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("text", InputField{
		Name:        "text",
		Order:       0,
		FieldType:   FieldType{Primitive: TypeString, Repetition: Required},
		Description: ptrStr("The input text"),
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	textField := props["text"].(map[string]any)
	assert.Equal(t, "The input text", textField["description"])
}

func TestInputNumericConstraints(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("temperature", InputField{
		Name:      "temperature",
		Order:     0,
		FieldType: FieldType{Primitive: TypeFloat, Repetition: Required},
		GE:        ptrFloat(0.0),
		LE:        ptrFloat(1.0),
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	tempField := props["temperature"].(map[string]any)
	assert.Equal(t, float64(0), tempField["minimum"])
	assert.Equal(t, float64(1), tempField["maximum"])
}

func TestInputStringConstraints(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("name", InputField{
		Name:      "name",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Required},
		MinLength: ptrUint(1),
		MaxLength: ptrUint(100),
		Regex:     ptrStr("^[a-z]+$"),
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	nameField := props["name"].(map[string]any)
	assert.Equal(t, float64(1), nameField["minLength"])
	assert.Equal(t, float64(100), nameField["maxLength"])
	assert.Equal(t, "^[a-z]+$", nameField["pattern"])
}

func TestInputDeprecated(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("old_param", InputField{
		Name:       "old_param",
		Order:      0,
		FieldType:  FieldType{Primitive: TypeString, Repetition: Required},
		Deprecated: ptrBool(true),
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	field := props["old_param"].(map[string]any)
	assert.Equal(t, true, field["deprecated"])
}

func TestInputXOrder(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("first", InputField{
		Name:      "first",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Required},
	})
	inputs.Set("second", InputField{
		Name:      "second",
		Order:     1,
		FieldType: FieldType{Primitive: TypeInteger, Repetition: Required},
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	assert.Equal(t, float64(0), props["first"].(map[string]any)["x-order"])
	assert.Equal(t, float64(1), props["second"].(map[string]any)["x-order"])
}

func TestInputRepeatedType(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("items", InputField{
		Name:      "items",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Repeated},
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	itemsField := props["items"].(map[string]any)
	assert.Equal(t, "array", itemsField["type"])
	items := itemsField["items"].(map[string]any)
	assert.Equal(t, "string", items["type"])
}

// ---------------------------------------------------------------------------
// Tests: Choices / Enums
// ---------------------------------------------------------------------------

func TestChoicesGenerateEnum(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("color", InputField{
		Name:      "color",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Required},
		Choices: []DefaultValue{
			{Kind: DefaultString, Str: "red"},
			{Kind: DefaultString, Str: "blue"},
		},
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)

	// Enum schema created
	schemas := getPath(spec, "components", "schemas").(map[string]any)
	colorEnum := schemas["Color"].(map[string]any)
	assert.Equal(t, "Color", colorEnum["title"])
	assert.Equal(t, "string", colorEnum["type"])
	assert.Equal(t, []any{"red", "blue"}, colorEnum["enum"])

	// Property uses allOf $ref
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)
	colorProp := props["color"].(map[string]any)
	allOf := colorProp["allOf"].([]any)
	assert.Len(t, allOf, 1)
	ref := allOf[0].(map[string]any)
	assert.Equal(t, "#/components/schemas/Color", ref["$ref"])
}

func TestIntegerChoices(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("size", InputField{
		Name:      "size",
		Order:     0,
		FieldType: FieldType{Primitive: TypeInteger, Repetition: Required},
		Choices: []DefaultValue{
			{Kind: DefaultInt, Int: 256},
			{Kind: DefaultInt, Int: 512},
			{Kind: DefaultInt, Int: 1024},
		},
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	schemas := getPath(spec, "components", "schemas").(map[string]any)
	sizeEnum := schemas["Size"].(map[string]any)
	assert.Equal(t, "integer", sizeEnum["type"])
	// JSON numbers are float64
	assert.Equal(t, []any{float64(256), float64(512), float64(1024)}, sizeEnum["enum"])
}

// ---------------------------------------------------------------------------
// Tests: Output types
// ---------------------------------------------------------------------------

func TestOutputSingle(t *testing.T) {
	spec := parseSpec(t, simplePredictor())
	output := getPath(spec, "components", "schemas", "Output").(map[string]any)
	assert.Equal(t, "Output", output["title"])
	assert.Equal(t, "string", output["type"])
}

func TestOutputList(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputList, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	output := getPath(spec, "components", "schemas", "Output").(map[string]any)
	assert.Equal(t, "Output", output["title"])
	assert.Equal(t, "array", output["type"])
	items := output["items"].(map[string]any)
	assert.Equal(t, "string", items["type"])
}

func TestOutputIterator(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputIterator, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	output := getPath(spec, "components", "schemas", "Output").(map[string]any)
	assert.Equal(t, "array", output["type"])
	assert.Equal(t, "iterator", output["x-cog-array-type"])
}

func TestOutputConcatenateIterator(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputConcatenateIterator, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	output := getPath(spec, "components", "schemas", "Output").(map[string]any)
	assert.Equal(t, "array", output["type"])
	assert.Equal(t, "iterator", output["x-cog-array-type"])
	assert.Equal(t, "concatenate", output["x-cog-array-display"])
}

func TestOutputObject(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	fields := NewOrderedMap[string, ObjectField]()
	fields.Set("name", ObjectField{
		FieldType: FieldType{Primitive: TypeString, Repetition: Required},
	})
	fields.Set("score", ObjectField{
		FieldType: FieldType{Primitive: TypeFloat, Repetition: Required},
	})
	fields.Set("notes", ObjectField{
		FieldType: FieldType{Primitive: TypeString, Repetition: Optional},
		Default:   &DefaultValue{Kind: DefaultNone},
	})

	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputObject, Fields: fields},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	output := getPath(spec, "components", "schemas", "Output").(map[string]any)
	assert.Equal(t, "object", output["type"])
	props := output["properties"].(map[string]any)

	// name
	nameField := props["name"].(map[string]any)
	assert.Equal(t, "string", nameField["type"])
	assert.Equal(t, "Name", nameField["title"])

	// score
	scoreField := props["score"].(map[string]any)
	assert.Equal(t, "number", scoreField["type"])

	// notes — nullable
	notesField := props["notes"].(map[string]any)
	assert.Equal(t, true, notesField["nullable"])

	// Required should include name and score but not notes
	required := output["required"].([]any)
	assert.Contains(t, required, "name")
	assert.Contains(t, required, "score")
	assert.NotContains(t, required, "notes")
}

func TestOutputPath(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	p := TypePath
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	output := getPath(spec, "components", "schemas", "Output").(map[string]any)
	assert.Equal(t, "string", output["type"])
	assert.Equal(t, "uri", output["format"])
}

// ---------------------------------------------------------------------------
// Tests: Post-processing
// ---------------------------------------------------------------------------

func TestRemoveTitleNextToRef(t *testing.T) {
	schema := map[string]any{
		"title": "Foo",
		"$ref":  "#/components/schemas/Bar",
	}
	removeTitleNextToRef(schema)
	assert.Nil(t, schema["title"])
	assert.Equal(t, "#/components/schemas/Bar", schema["$ref"])
}

func TestRemoveTitleNextToRefNested(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"inner": map[string]any{
				"title": "Inner",
				"$ref":  "#/components/schemas/Foo",
			},
		},
	}
	removeTitleNextToRef(schema)
	inner := schema["properties"].(map[string]any)["inner"].(map[string]any)
	assert.Nil(t, inner["title"])
}

func TestFixNullableAnyOf(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "null"},
		},
	}
	fixNullableAnyOf(schema)
	assert.Nil(t, schema["anyOf"])
	assert.Equal(t, "string", schema["type"])
	assert.Equal(t, true, schema["nullable"])
}

func TestFixNullableAnyOfNoOp(t *testing.T) {
	// anyOf with no null should be left alone
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}
	fixNullableAnyOf(schema)
	assert.NotNil(t, schema["anyOf"])
	assert.Nil(t, schema["nullable"])
}

// ---------------------------------------------------------------------------
// Tests: Title case helpers
// ---------------------------------------------------------------------------

func TestTitleCaseWords(t *testing.T) {
	assert.Equal(t, "Hello World", TitleCase("hello_world"))
	assert.Equal(t, "Segmented Image", TitleCase("segmented_image"))
	assert.Equal(t, "Name", TitleCase("name"))
}

func TestTitleCaseSingleWord(t *testing.T) {
	assert.Equal(t, "Prediction_id", TitleCaseSingle("prediction_id"))
	assert.Equal(t, "Color", TitleCaseSingle("color"))
	assert.Equal(t, "", TitleCaseSingle(""))
}

// ---------------------------------------------------------------------------
// Tests: JSON output is valid and parseable
// ---------------------------------------------------------------------------

func TestOutputIsValidJSON(t *testing.T) {
	data, err := GenerateOpenAPISchema(simplePredictor())
	require.NoError(t, err)

	var parsed any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.NotNil(t, parsed)
}

// ---------------------------------------------------------------------------
// Tests: Multiple inputs with various types
// ---------------------------------------------------------------------------

func TestMultipleInputTypes(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	inputs.Set("text", InputField{
		Name:      "text",
		Order:     0,
		FieldType: FieldType{Primitive: TypeString, Repetition: Required},
	})
	inputs.Set("count", InputField{
		Name:      "count",
		Order:     1,
		FieldType: FieldType{Primitive: TypeInteger, Repetition: Required},
		Default:   &DefaultValue{Kind: DefaultInt, Int: 10},
	})
	inputs.Set("image", InputField{
		Name:      "image",
		Order:     2,
		FieldType: FieldType{Primitive: TypePath, Repetition: Required},
	})
	inputs.Set("flag", InputField{
		Name:      "flag",
		Order:     3,
		FieldType: FieldType{Primitive: TypeBool, Repetition: Required},
		Default:   &DefaultValue{Kind: DefaultBool, Bool: false},
	})
	inputs.Set("secret_key", InputField{
		Name:      "secret_key",
		Order:     4,
		FieldType: FieldType{Primitive: TypeSecret, Repetition: Optional},
		Default:   &DefaultValue{Kind: DefaultNone},
	})

	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	props := getPath(spec, "components", "schemas", "Input", "properties").(map[string]any)

	// text - string
	textField := props["text"].(map[string]any)
	assert.Equal(t, "string", textField["type"])

	// count - integer with default
	countField := props["count"].(map[string]any)
	assert.Equal(t, "integer", countField["type"])
	assert.Equal(t, float64(10), countField["default"])

	// image - path (URI)
	imageField := props["image"].(map[string]any)
	assert.Equal(t, "string", imageField["type"])
	assert.Equal(t, "uri", imageField["format"])

	// flag - boolean
	flagField := props["flag"].(map[string]any)
	assert.Equal(t, "boolean", flagField["type"])

	// secret_key - secret
	secretField := props["secret_key"].(map[string]any)
	assert.Equal(t, "string", secretField["type"])
	assert.Equal(t, "password", secretField["format"])
	assert.Equal(t, true, secretField["writeOnly"])
	assert.Equal(t, true, secretField["x-cog-secret"])
	assert.Equal(t, true, secretField["nullable"])

	// Only text and image should be required (count has default, flag has default, secret has default)
	required := getPath(spec, "components", "schemas", "Input", "required").([]any)
	assert.Contains(t, required, "text")
	assert.Contains(t, required, "image")
	assert.NotContains(t, required, "count")
	assert.NotContains(t, required, "flag")
	assert.NotContains(t, required, "secret_key")
}

// ---------------------------------------------------------------------------
// Tests: Edge cases
// ---------------------------------------------------------------------------

func TestNoInputs(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	p := TypeString
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputSingle, Primitive: &p},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	input := getPath(spec, "components", "schemas", "Input").(map[string]any)
	assert.Equal(t, "object", input["type"])
	// required should not be present when there are no required fields
	assert.Nil(t, input["required"])
}

func TestOutputObjectNoFields(t *testing.T) {
	inputs := NewOrderedMap[string, InputField]()
	info := &PredictorInfo{
		Inputs: inputs,
		Output: OutputType{Kind: OutputObject},
		Mode:   ModePredict,
	}

	spec := parseSpec(t, info)
	output := getPath(spec, "components", "schemas", "Output").(map[string]any)
	assert.Equal(t, "object", output["type"])
}

// ---------------------------------------------------------------------------
// Tests: orderedMapAny JSON output preserves insertion order
// ---------------------------------------------------------------------------

func TestOrderedMapAnyJSON(t *testing.T) {
	m := newOrderedMapAny()
	m.Set("z", 1)
	m.Set("a", 2)
	m.Set("m", 3)

	data, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t, `{"z":1,"a":2,"m":3}`, string(data))
}

func TestOrderedMapAnyDelete(t *testing.T) {
	m := newOrderedMapAny()
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("c", 3)
	m.Delete("b")

	data, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1,"c":3}`, string(data))
}
