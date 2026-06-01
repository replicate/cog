package python

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

func TestParseTypeFromStringHandlesNestedGenericsUnionsAndForwardRefs(t *testing.T) {
	tests := []struct {
		name   string
		typeIn string
		want   schema.TypeAnnotation
	}{
		{
			name:   "nested generic",
			typeIn: "dict[str, list[int]]",
			want: schema.TypeAnnotation{Kind: schema.TypeAnnotGeneric, Name: "dict", Args: []schema.TypeAnnotation{
				{Kind: schema.TypeAnnotSimple, Name: "str"},
				{Kind: schema.TypeAnnotGeneric, Name: "list", Args: []schema.TypeAnnotation{{Kind: schema.TypeAnnotSimple, Name: "int"}}},
			}},
		},
		{
			name:   "pep 604 union",
			typeIn: "str | None",
			want: schema.TypeAnnotation{Kind: schema.TypeAnnotUnion, Args: []schema.TypeAnnotation{
				{Kind: schema.TypeAnnotSimple, Name: "str"},
				{Kind: schema.TypeAnnotSimple, Name: "None"},
			}},
		},
		{
			name:   "quoted forward ref union",
			typeIn: `"TreeNode | None"`,
			want: schema.TypeAnnotation{Kind: schema.TypeAnnotUnion, Args: []schema.TypeAnnotation{
				{Kind: schema.TypeAnnotSimple, Name: "TreeNode"},
				{Kind: schema.TypeAnnotSimple, Name: "None"},
			}},
		},
		{
			name:   "qualified identifier",
			typeIn: "np.ndarray",
			want:   schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: "np.ndarray"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseTypeFromString(tt.typeIn)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseTypeFromStringRejectsInvalidIdentifiers(t *testing.T) {
	_, ok := parseTypeFromString("dict[str, bad-name]")
	require.False(t, ok)
}

func TestParseTypeAnnotationHandlesTreeSitterNodes(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   schema.TypeAnnotation
	}{
		{
			name:   "pep 604 union node",
			source: "def f(value: str | None) -> None:\n    pass\n",
			want: schema.TypeAnnotation{Kind: schema.TypeAnnotUnion, Args: []schema.TypeAnnotation{
				{Kind: schema.TypeAnnotSimple, Name: "str"},
				{Kind: schema.TypeAnnotSimple, Name: "None"},
			}},
		},
		{
			name:   "string forward ref node",
			source: "def f(value: \"TreeNode | None\") -> None:\n    pass\n",
			want: schema.TypeAnnotation{Kind: schema.TypeAnnotUnion, Args: []schema.TypeAnnotation{
				{Kind: schema.TypeAnnotSimple, Name: "TreeNode"},
				{Kind: schema.TypeAnnotSimple, Name: "None"},
			}},
		},
		{
			name:   "qualified attribute node",
			source: "def f(value: np.ndarray) -> None:\n    pass\n",
			want:   schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: "np.ndarray"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := parsePythonTestModule(t, tt.source)
			typedParam := findFirstNodeByType(parsed.root, "typed_parameter")
			require.NotNil(t, typedParam)
			typeNode := typedParam.ChildByFieldName("type")
			require.NotNil(t, typeNode)

			got, err := parseTypeAnnotation(typeNode, parsed.source)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
