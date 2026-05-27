package python

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

func TestCollectModuleScopeCapturesResolvableAssignmentsOnly(t *testing.T) {
	parsed := parsePythonTestModule(t, `
DESCRIPTION = "a useful input"
COUNT = 3
COLORS = ["red", "blue"]
LIMITS = {"low": 1, "high": 10}
CALL_VALUE = make_value()
`)

	scope := collectModuleScope(parsed.root, parsed.source)

	require.Equal(t, schema.DefaultString, scope["DESCRIPTION"].Kind)
	require.Equal(t, "a useful input", scope["DESCRIPTION"].Str)
	require.Equal(t, schema.DefaultInt, scope["COUNT"].Kind)
	require.Equal(t, int64(3), scope["COUNT"].Int)
	require.Equal(t, schema.DefaultList, scope["COLORS"].Kind)
	require.Len(t, scope["COLORS"].List, 2)
	require.Equal(t, schema.DefaultDict, scope["LIMITS"].Kind)
	require.NotContains(t, scope, "CALL_VALUE")
}

func TestResolveExpressionsUseModuleScopeFallbacks(t *testing.T) {
	parsed := parsePythonTestModule(t, `
DESCRIPTION = "from scope"
DEFAULT_VALUE = 4
COLORS = ["red"]
MORE_COLORS = ["blue"]
LIMITS = {"low": 1, "high": 10}
`)
	scope := collectModuleScope(parsed.root, parsed.source)

	stringSource, stringNode := requireAssignmentRight(t, "DESCRIPTION")
	resolvedString, ok := resolveStringExpr(stringNode, stringSource, scope)
	require.True(t, ok)
	require.Equal(t, "from scope", resolvedString)

	defaultSource, defaultNode := requireAssignmentRight(t, "DEFAULT_VALUE")
	resolvedDefault, ok := resolveDefaultExpr(defaultNode, defaultSource, scope)
	require.True(t, ok)
	require.Equal(t, schema.DefaultInt, resolvedDefault.Kind)
	require.Equal(t, int64(4), resolvedDefault.Int)

	choicesSource, choicesNode := requireAssignmentRight(t, "COLORS + MORE_COLORS")
	choices, ok := resolveChoicesExpr(choicesNode, choicesSource, scope)
	require.True(t, ok)
	require.Len(t, choices, 2)
	require.Equal(t, "red", choices[0].Str)
	require.Equal(t, "blue", choices[1].Str)

	keysSource, keysNode := requireAssignmentRight(t, "list(LIMITS.keys())")
	keys, ok := resolveChoicesExpr(keysNode, keysSource, scope)
	require.True(t, ok)
	require.Len(t, keys, 2)
	require.Equal(t, "low", keys[0].Str)
	require.Equal(t, "high", keys[1].Str)
}

func TestResolveChoicesExprRejectsUnsupportedExpressions(t *testing.T) {
	parsed := parsePythonTestModule(t, `COLORS = ["red"]`)
	scope := collectModuleScope(parsed.root, parsed.source)
	source, node := requireAssignmentRight(t, "tuple(COLORS)")

	_, ok := resolveChoicesExpr(node, source, scope)
	require.False(t, ok)
}
