package predict

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

// unionInputSchema builds an OpenAPI doc whose single input field `value`
// is a union of string and number. The variant order is configurable so we
// can exercise both `str | float` (string first) and `float | str` (number
// first), which resolve differently via resolveSchemaType.
func unionInputSchema(numberFirst bool) *openapi3.T {
	stringRef := openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}}
	numberRef := openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"number"}}}
	anyOf := openapi3.SchemaRefs{&stringRef, &numberRef}
	if numberFirst {
		anyOf = openapi3.SchemaRefs{&numberRef, &stringRef}
	}
	valueSchema := &openapi3.Schema{AnyOf: anyOf}
	inputSchema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"value": {Value: valueSchema},
		},
	}
	return &openapi3.T{
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				"Input": {Value: inputSchema},
			},
		},
	}
}

func TestNewInputsForMode_UnionParsesNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		numberFirst bool
		val         string
		wantInt     *int32
		wantFlt     *float32
		wantStr     *string
	}{
		// str | float (string member first)
		{name: "str|float integer", val: "1", wantInt: ptrI32(1)},
		{name: "str|float float", val: "1.5", wantFlt: ptrF32(1.5)},
		{name: "str|float string", val: "hello", wantStr: ptrStr("hello")},
		// float | str (number member first) -- must still fall back to string
		{name: "float|str integer", numberFirst: true, val: "1", wantInt: ptrI32(1)},
		{name: "float|str float", numberFirst: true, val: "1.5", wantFlt: ptrF32(1.5)},
		{name: "float|str string", numberFirst: true, val: "hello", wantStr: ptrStr("hello")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			schema := unionInputSchema(tt.numberFirst)
			inputs, err := NewInputsForMode(map[string][]string{"value": {tt.val}}, schema, false)
			require.NoError(t, err)

			got := inputs["value"]
			switch {
			case tt.wantInt != nil:
				require.NotNil(t, got.Int)
				require.Equal(t, *tt.wantInt, *got.Int)
			case tt.wantFlt != nil:
				require.NotNil(t, got.Float)
				require.Equal(t, *tt.wantFlt, *got.Float)
			case tt.wantStr != nil:
				require.NotNil(t, got.String)
				require.Equal(t, *tt.wantStr, *got.String)
			}
		})
	}
}

// unionInputSchemaOf builds an OpenAPI doc whose single input field `value`
// is a union (anyOf) of the given JSON Schema types, in the given order.
func unionInputSchemaOf(types ...string) *openapi3.T {
	anyOf := make(openapi3.SchemaRefs, len(types))
	for i, t := range types {
		anyOf[i] = &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{t}}}
	}
	inputSchema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"value": {Value: &openapi3.Schema{AnyOf: anyOf}},
		},
	}
	return &openapi3.T{
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				"Input": {Value: inputSchema},
			},
		},
	}
}

func TestNewInputsForMode_UnionIntFloatAndStrInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		types   []string
		val     string
		wantInt *int32
		wantFlt *float32
		wantStr *string
	}{
		// int | float: integer member resolves first; a fractional value must
		// fall back to the float member instead of erroring.
		{name: "int|float integer", types: []string{"integer", "number"}, val: "1", wantInt: ptrI32(1)},
		{name: "int|float fractional", types: []string{"integer", "number"}, val: "1.5", wantFlt: ptrF32(1.5)},
		// str | int: string resolves first; a fractional value is not valid for
		// the integer member and must fall back to the string member.
		{name: "str|int integer", types: []string{"string", "integer"}, val: "1", wantInt: ptrI32(1)},
		{name: "str|int fractional", types: []string{"string", "integer"}, val: "1.5", wantStr: ptrStr("1.5")},
		{name: "str|int string", types: []string{"string", "integer"}, val: "hello", wantStr: ptrStr("hello")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			schema := unionInputSchemaOf(tt.types...)
			inputs, err := NewInputsForMode(map[string][]string{"value": {tt.val}}, schema, false)
			require.NoError(t, err)

			got := inputs["value"]
			switch {
			case tt.wantInt != nil:
				require.NotNil(t, got.Int, "expected int")
				require.Equal(t, *tt.wantInt, *got.Int)
			case tt.wantFlt != nil:
				require.NotNil(t, got.Float, "expected float")
				require.Equal(t, *tt.wantFlt, *got.Float)
			case tt.wantStr != nil:
				require.NotNil(t, got.String, "expected string")
				require.Equal(t, *tt.wantStr, *got.String)
			}
		})
	}
}

func TestNewInputsForMode_UnionParsesBool(t *testing.T) {
	t.Parallel()

	for _, types := range [][]string{{"string", "boolean"}, {"boolean", "string"}} {
		for _, val := range []string{"true", "false"} {
			schema := unionInputSchemaOf(types...)
			inputs, err := NewInputsForMode(map[string][]string{"value": {val}}, schema, false)
			require.NoError(t, err)
			require.NotNil(t, inputs["value"].Bool)
			require.Equal(t, val == "true", *inputs["value"].Bool)
		}
	}
}

func TestNewInputsForMode_MixedNumericBoolUnion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		types []string
		val   string
		want  any
	}{
		{name: "int before bool parses bool", types: []string{"integer", "boolean"}, val: "true", want: true},
		{name: "bool before int parses int", types: []string{"boolean", "integer"}, val: "1", want: int32(1)},
		{name: "bool before float parses float", types: []string{"boolean", "number"}, val: "1.5", want: float32(1.5)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs, err := NewInputsForMode(map[string][]string{"value": {tt.val}}, unionInputSchemaOf(tt.types...), false)
			require.NoError(t, err)
			got, err := inputs.toMap()
			require.NoError(t, err)
			require.Equal(t, tt.want, got["value"])
		})
	}
}

func TestNewInputsForMode_BoolUnionDoesNotParseAliases(t *testing.T) {
	t.Parallel()

	schema := unionInputSchemaOf("string", "boolean")
	for _, val := range []string{"1", "0", "t", "f", "TRUE"} {
		inputs, err := NewInputsForMode(map[string][]string{"value": {val}}, schema, false)
		require.NoError(t, err)
		require.NotNil(t, inputs["value"].String)
		require.Equal(t, val, *inputs["value"].String)
	}
}

func TestNewInputsForMode_ArrayUnionCoercesItem(t *testing.T) {
	t.Parallel()

	arraySchema := func(itemType string) *openapi3.SchemaRef {
		return &openapi3.SchemaRef{Value: &openapi3.Schema{
			Type:  &openapi3.Types{"array"},
			Items: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{itemType}}},
		}}
	}
	inputSchema := &openapi3.Schema{
		Type: &openapi3.Types{"object"},
		Properties: openapi3.Schemas{
			"values": {Value: &openapi3.Schema{AnyOf: openapi3.SchemaRefs{arraySchema("integer"), arraySchema("number")}}},
		},
	}
	schema := &openapi3.T{Components: &openapi3.Components{Schemas: openapi3.Schemas{"Input": {Value: inputSchema}}}}

	inputs, err := NewInputsForMode(map[string][]string{"values": {"1.5"}}, schema, false)
	require.NoError(t, err)
	require.Equal(t, []any{float32(1.5)}, *inputs["values"].Array)
}

func TestNewInputsForMode_ArrayStringUnionPreservesString(t *testing.T) {
	t.Parallel()

	valueSchema := &openapi3.Schema{AnyOf: openapi3.SchemaRefs{
		{Value: &openapi3.Schema{
			Type:  &openapi3.Types{"array"},
			Items: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
		}},
		{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
	}}
	inputSchema := &openapi3.Schema{Type: &openapi3.Types{"object"}, Properties: openapi3.Schemas{
		"value": {Value: valueSchema},
	}}
	schema := &openapi3.T{Components: &openapi3.Components{Schemas: openapi3.Schemas{"Input": {Value: inputSchema}}}}

	inputs, err := NewInputsForMode(map[string][]string{"value": {"hello"}}, schema, false)
	require.NoError(t, err)
	require.Equal(t, "hello", *inputs["value"].String)

	inputs, err = NewInputsForMode(map[string][]string{"value": {"[1]"}}, schema, false)
	require.NoError(t, err)
	require.NotNil(t, inputs["value"].Json)
}

func TestNewInputsForModeWithoutComponentsPreservesValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema *openapi3.T
	}{
		{name: "nil schema"},
		{name: "missing components", schema: &openapi3.T{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs, err := NewInputsForMode(map[string][]string{
				"prompt": {"hello"},
				"image":  {"@image.png"},
				"values": {"one", "two"},
			}, tt.schema, false)
			require.NoError(t, err)
			require.NotNil(t, inputs["prompt"].String)
			require.Equal(t, "hello", *inputs["prompt"].String)
			require.NotNil(t, inputs["image"].File)
			require.Equal(t, "image.png", *inputs["image"].File)
			require.NotNil(t, inputs["values"].Array)
			require.Equal(t, []any{"one", "two"}, *inputs["values"].Array)
		})
	}
}

func TestNewInputsForModeCoercesOneOfValue(t *testing.T) {
	t.Parallel()

	schema := validationTestSchema(t)
	schema.Components.Schemas["Input"].Value.Properties["choice"] = &openapi3.SchemaRef{Value: &openapi3.Schema{OneOf: openapi3.SchemaRefs{
		{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
	}}}
	inputs, err := NewInputsForMode(map[string][]string{"choice": {"1"}}, schema, false)
	require.NoError(t, err)
	require.NotNil(t, inputs["choice"].Int)
	require.Equal(t, int32(1), *inputs["choice"].Int)
}

func TestSchemaAcceptsNumber(t *testing.T) {
	t.Parallel()

	require.True(t, schemaAcceptsNumber(&openapi3.Schema{Type: &openapi3.Types{"number"}}))
	require.True(t, schemaAcceptsNumber(&openapi3.Schema{Type: &openapi3.Types{"integer"}}))
	require.False(t, schemaAcceptsNumber(&openapi3.Schema{Type: &openapi3.Types{"string"}}))
	require.False(t, schemaAcceptsNumber(nil))

	union := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"number"}}},
		},
	}
	require.True(t, schemaAcceptsNumber(union))

	stringOnlyUnion := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
		},
	}
	require.False(t, schemaAcceptsNumber(stringOnlyUnion))
}

func TestSchemaAcceptsString(t *testing.T) {
	t.Parallel()

	require.True(t, schemaAcceptsString(&openapi3.Schema{Type: &openapi3.Types{"string"}}))
	require.False(t, schemaAcceptsString(&openapi3.Schema{Type: &openapi3.Types{"number"}}))
	require.False(t, schemaAcceptsString(nil))

	union := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"number"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		},
	}
	require.True(t, schemaAcceptsString(union))

	numericOnlyUnion := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"number"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
		},
	}
	require.False(t, schemaAcceptsString(numericOnlyUnion))
}

func TestSchemaAcceptsFloat(t *testing.T) {
	t.Parallel()

	require.True(t, schemaAcceptsFloat(&openapi3.Schema{Type: &openapi3.Types{"number"}}))
	require.False(t, schemaAcceptsFloat(&openapi3.Schema{Type: &openapi3.Types{"integer"}}))
	require.False(t, schemaAcceptsFloat(&openapi3.Schema{Type: &openapi3.Types{"string"}}))
	require.False(t, schemaAcceptsFloat(nil))

	intFloatUnion := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"number"}}},
		},
	}
	require.True(t, schemaAcceptsFloat(intFloatUnion))

	strIntUnion := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
		},
	}
	require.False(t, schemaAcceptsFloat(strIntUnion))
}

func TestSchemaAcceptsBool(t *testing.T) {
	t.Parallel()

	require.True(t, schemaAcceptsBool(&openapi3.Schema{Type: &openapi3.Types{"boolean"}}))
	require.False(t, schemaAcceptsBool(&openapi3.Schema{Type: &openapi3.Types{"string"}}))
	require.False(t, schemaAcceptsBool(nil))

	union := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
			{Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
		},
	}
	require.True(t, schemaAcceptsBool(union))
}

func TestCoerceScalarValueMixedUnion(t *testing.T) {
	t.Parallel()

	schema := &openapi3.Schema{AnyOf: openapi3.SchemaRefs{
		{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
		{Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
	}}
	require.Equal(t, true, coerceScalarValue("true", schema))
	require.Equal(t, int32(1), coerceScalarValue("1", schema))
}

func TestCoerceScalarValueRespectsUnionConstraints(t *testing.T) {
	t.Parallel()

	minimum := 10.0
	numericUnion := &openapi3.Schema{AnyOf: openapi3.SchemaRefs{
		{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		{Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Min: &minimum}},
	}}
	require.Equal(t, "5", coerceScalarValue("5", numericUnion))
	require.Equal(t, int32(15), coerceScalarValue("15", numericUnion))

	boolUnion := &openapi3.Schema{AnyOf: openapi3.SchemaRefs{
		{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		{Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}, Enum: []any{true}}},
	}}
	require.Equal(t, "false", coerceScalarValue("false", boolUnion))
	require.Equal(t, true, coerceScalarValue("true", boolUnion))
}

func TestNewInputsForMode_CoercesBool(t *testing.T) {
	t.Parallel()

	schema := validationTestSchema(t)
	for _, val := range []string{"true", "false"} {
		inputs, err := NewInputsForMode(map[string][]string{"prompt": {"hi"}, "flag": {val}}, schema, false)
		require.NoError(t, err)
		require.NotNil(t, inputs["flag"].Bool, "flag=%s should coerce to bool", val)
		require.Equal(t, val == "true", *inputs["flag"].Bool)
		// The coerced value must satisfy preflight validation.
		require.NoError(t, ValidateInputsForMode(inputs, schema, false))
	}
}

func TestNewInputsForMode_CoercesRepeatedIntArray(t *testing.T) {
	t.Parallel()

	schema := validationTestSchema(t)
	inputs, err := NewInputsForMode(map[string][]string{"prompt": {"hi"}, "nums": {"1", "2"}}, schema, false)
	require.NoError(t, err)
	require.NotNil(t, inputs["nums"].Array)
	require.Equal(t, []any{int32(1), int32(2)}, *inputs["nums"].Array)
	require.NoError(t, ValidateInputsForMode(inputs, schema, false))
}

func TestNewInputsForMode_CoercesSingleIntArray(t *testing.T) {
	t.Parallel()

	schema := validationTestSchema(t)
	inputs, err := NewInputsForMode(map[string][]string{"prompt": {"hi"}, "nums": {"1"}}, schema, false)
	require.NoError(t, err)
	require.NotNil(t, inputs["nums"].Array)
	require.Equal(t, []any{int32(1)}, *inputs["nums"].Array)
	require.NoError(t, ValidateInputsForMode(inputs, schema, false))
}

func ptrI32(v int32) *int32     { return &v }
func ptrF32(v float32) *float32 { return &v }
func ptrStr(v string) *string   { return &v }
