package python

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

func TestParseDefaultValueHandlesScalarLiterals(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		kind     schema.DefaultKind
		assertFn func(t *testing.T, value schema.DefaultValue)
	}{
		{name: "none", expr: "None", kind: schema.DefaultNone},
		{name: "true", expr: "True", kind: schema.DefaultBool, assertFn: func(t *testing.T, value schema.DefaultValue) { require.True(t, value.Bool) }},
		{name: "false", expr: "False", kind: schema.DefaultBool, assertFn: func(t *testing.T, value schema.DefaultValue) { require.False(t, value.Bool) }},
		{name: "integer", expr: "42", kind: schema.DefaultInt, assertFn: func(t *testing.T, value schema.DefaultValue) { require.Equal(t, int64(42), value.Int) }},
		{name: "negative integer", expr: "-7", kind: schema.DefaultInt, assertFn: func(t *testing.T, value schema.DefaultValue) { require.Equal(t, int64(-7), value.Int) }},
		{name: "float", expr: "1.25", kind: schema.DefaultFloat, assertFn: func(t *testing.T, value schema.DefaultValue) { require.InDelta(t, 1.25, value.Float, 0.0001) }},
		{name: "string", expr: "'hello'", kind: schema.DefaultString, assertFn: func(t *testing.T, value schema.DefaultValue) { require.Equal(t, "hello", value.Str) }},
		{name: "raw string", expr: `r"^a+$"`, kind: schema.DefaultString, assertFn: func(t *testing.T, value schema.DefaultValue) { require.Equal(t, "^a+$", value.Str) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, node := requireAssignmentRight(t, tt.expr)
			value, ok := parseDefaultValue(node, source)
			require.True(t, ok)
			require.Equal(t, tt.kind, value.Kind)
			if tt.assertFn != nil {
				tt.assertFn(t, value)
			}
		})
	}
}

func TestParseDefaultValueHandlesCompoundLiterals(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		source, node := requireAssignmentRight(t, "['a', 1]")
		value, ok := parseDefaultValue(node, source)
		require.True(t, ok)
		require.Equal(t, schema.DefaultList, value.Kind)
		require.Len(t, value.List, 2)
		require.Equal(t, "a", value.List[0].Str)
		require.Equal(t, int64(1), value.List[1].Int)
	})

	t.Run("tuple as list", func(t *testing.T) {
		source, node := requireAssignmentRight(t, "('a', 2)")
		value, ok := parseDefaultValue(node, source)
		require.True(t, ok)
		require.Equal(t, schema.DefaultList, value.Kind)
		require.Len(t, value.List, 2)
		require.Equal(t, "a", value.List[0].Str)
		require.Equal(t, int64(2), value.List[1].Int)
	})

	t.Run("dictionary", func(t *testing.T) {
		source, node := requireAssignmentRight(t, "{'one': 1, 'two': 2}")
		value, ok := parseDefaultValue(node, source)
		require.True(t, ok)
		require.Equal(t, schema.DefaultDict, value.Kind)
		require.Len(t, value.DictKeys, 2)
		require.Len(t, value.DictVals, 2)
		require.Equal(t, "one", value.DictKeys[0].Str)
		require.Equal(t, int64(1), value.DictVals[0].Int)
	})

	t.Run("set", func(t *testing.T) {
		source, node := requireAssignmentRight(t, "{'red', 'blue'}")
		value, ok := parseDefaultValue(node, source)
		require.True(t, ok)
		require.Equal(t, schema.DefaultSet, value.Kind)
		require.Len(t, value.List, 2)
		require.Equal(t, "red", value.List[0].Str)
		require.Equal(t, "blue", value.List[1].Str)
	})
}

func TestParseDefaultValueRejectsUnresolvableExpressions(t *testing.T) {
	source, node := requireAssignmentRight(t, "make_default()")

	_, ok := parseDefaultValue(node, source)
	require.False(t, ok)
}
