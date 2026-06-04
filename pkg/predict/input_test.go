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

func ptrI32(v int32) *int32     { return &v }
func ptrF32(v float32) *float32 { return &v }
func ptrStr(v string) *string   { return &v }
