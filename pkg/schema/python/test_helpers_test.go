package python

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	sitterpython "github.com/smacker/go-tree-sitter/python"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/schema"
)

type parsedPythonModule struct {
	source []byte
	tree   *sitter.Tree
	root   *sitter.Node
}

func parsePythonTestModule(t *testing.T, source string) parsedPythonModule {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(sitterpython.GetLanguage())
	sourceBytes := []byte(source)
	tree, err := parser.ParseCtx(context.Background(), nil, sourceBytes)
	require.NoError(t, err)
	return parsedPythonModule{source: sourceBytes, tree: tree, root: tree.RootNode()}
}

func findFirstNodeByType(root *sitter.Node, nodeType string) *sitter.Node {
	if root.Type() == nodeType {
		return root
	}
	for _, child := range NamedChildren(root) {
		if node := findFirstNodeByType(child, nodeType); node != nil {
			return node
		}
	}
	return nil
}

func requireAssignmentRight(t *testing.T, expr string) ([]byte, *sitter.Node) {
	t.Helper()
	parsed := parsePythonTestModule(t, "value = "+expr+"\n")
	_ = parsed.tree
	assign := findFirstNodeByType(parsed.root, "assignment")
	require.NotNil(t, assign)
	right := assign.ChildByFieldName("right")
	require.NotNil(t, right)
	return parsed.source, right
}

func newPythonFileContextForTest(t *testing.T, source string) *pythonFileContext {
	t.Helper()
	parsed := parsePythonTestModule(t, source)
	imports := CollectImports(parsed.root, parsed.source)
	scope := collectModuleScope(parsed.root, parsed.source)
	modelCtx := &modelParseContext{imports: imports, typedDicts: make(map[string]bool), loadedModules: make(map[string]ModuleSummary)}
	models := collectModelClasses(parsed.root, parsed.source, modelCtx)
	return &pythonFileContext{
		root:          parsed.root,
		source:        parsed.source,
		imports:       imports,
		moduleScope:   scope,
		inputRegistry: collectInputRegistry(parsed.root, parsed.source, imports, scope),
		modelClasses:  models,
		typedDicts:    modelCtx.typedDicts,
		mode:          schema.ModePredict,
		allowLegacy:   true,
		fileCache:     make(map[string]*pythonFileContext),
		loading:       make(map[string]bool),
	}
}
