package python

import (
	"context"
	"runtime"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	sitterpython "github.com/smacker/go-tree-sitter/python"

	schema "github.com/replicate/cog/pkg/schema"
)

func parseFuzzRoot(t *testing.T, source []byte) (*sitter.Tree, *sitter.Node) {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(sitterpython.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, nil
	}
	return tree, tree.RootNode()
}

// FuzzParsePredictor feeds arbitrary bytes as Python source to the parser.
// The parser should never panic regardless of input — it may return errors.
func FuzzParsePredictor(f *testing.F) {
	// Seed corpus: valid and invalid Python snippets.
	f.Add([]byte(`
from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, x: str) -> str:
        return x
`), "Predictor", uint8(0))

	f.Add([]byte(`
from cog import BasePredictor
from pydantic import BaseModel
class Output(BaseModel):
    text: str
    score: float = 0.0
class Predictor(BasePredictor):
    def predict(self, x: str) -> Output:
        pass
`), "Predictor", uint8(0))

	f.Add([]byte(`
from cog import BasePredictor
class Predictor(BasePredictor):
    def predict(self, x: str) -> dict[str, list[int]]:
        return {}
`), "Predictor", uint8(0))

	f.Add([]byte(`
from cog import BasePredictor, ConcatenateIterator
class Predictor(BasePredictor):
    def predict(self, x: str) -> ConcatenateIterator[str]:
        yield x
`), "Predictor", uint8(0))

	// Training mode.
	f.Add([]byte(`
from cog import BasePredictor
class Predictor(BasePredictor):
    def train(self, x: str) -> str:
        return x
`), "Predictor", uint8(1))

	// No predictor class at all.
	f.Add([]byte(`print("hello")`), "Predictor", uint8(0))

	// Empty source.
	f.Add([]byte{}, "Predictor", uint8(0))

	// Garbage bytes.
	f.Add([]byte{0xff, 0xfe, 0x00, 0x01, 0x80, 0x90}, "Predictor", uint8(0))

	f.Fuzz(func(t *testing.T, source []byte, targetRef string, modeRaw uint8) {
		// Cap input size: tree-sitter can exhibit O(n^2) behavior on
		// pathological inputs, causing the fuzz worker to exceed the
		// per-input grace period and fail with "context deadline exceeded".
		// Real Python source files are bounded; 64 KB is generous.
		if len(source) > 64*1024 {
			t.Skip("input too large")
		}

		mode := schema.ModePredict
		if modeRaw%2 == 1 {
			mode = schema.ModeTrain
		}

		// Must not panic regardless of input.
		_, _ = ParsePredictor(source, targetRef, mode, "")
	})
}

// FuzzParseTypeAnnotation exercises the type annotation parser with
// arbitrary annotation strings embedded in a predict signature.
func FuzzParseTypeAnnotation(f *testing.F) {
	types := []string{
		"str", "int", "float", "bool", "Path",
		"dict", "dict[str, int]", "dict[str, list[str]]",
		"list[str]", "list[dict[str, float]]",
		"Optional[str]", "Optional[dict[str, int]]",
		"Iterator[str]", "ConcatenateIterator[str]",
		"dict[str, dict[str, dict[str, int]]]",
		"Any", "None", "list",
	}
	for _, typ := range types {
		f.Add(typ)
	}

	f.Fuzz(func(t *testing.T, typeName string) {
		// Cap annotation length: pathologically long or deeply nested
		// type strings (e.g. "dict[dict[dict[..." repeated thousands
		// of times) can make tree-sitter exceed the fuzz worker's
		// grace period. Real type annotations are short.
		if len(typeName) > 1024 {
			t.Skip("input too large")
		}

		// Build a minimal predict.py with the fuzzed return type.
		source := []byte("from cog import BasePredictor\nfrom typing import *\nclass Predictor(BasePredictor):\n    def predict(self, x: str) -> " + typeName + ":\n        pass\n")
		// Must not panic.
		_, _ = ParsePredictor(source, "Predictor", schema.ModePredict, "")
	})
}

// FuzzCollectImportsAndModuleScope exercises the pre-target parser phases with
// arbitrary Python source. These helpers should tolerate malformed or partial
// modules and either collect known static information or ignore unsupported
// forms without panicking.
func FuzzCollectImportsAndModuleScope(f *testing.F) {
	seeds := []string{
		"from cog import Input\nVALUE = 'x'\n",
		"import os, numpy as np\nCHOICES = ['a', 'b']\n",
		"from .models import Output as ModelOutput\nLIMITS = {'low': 1}\n",
		"VALUE = make_value()\n",
		"",
		"\xff\xfe\x00\x01",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, source []byte) {
		if len(source) > 64*1024 {
			t.Skip("input too large")
		}

		tree, root := parseFuzzRoot(t, source)
		if root == nil {
			return
		}
		imports := CollectImports(root, source)
		scope := collectModuleScope(root, source)
		registry := collectInputRegistry(root, source, imports, scope)
		_ = registry
		runtime.KeepAlive(tree)
	})
}

// FuzzParseDefaultExpression wraps arbitrary text as an assignment RHS and
// exercises literal/default parsing plus module-scope expression resolution.
func FuzzParseDefaultExpression(f *testing.F) {
	seeds := []string{
		"None",
		"True",
		"-3",
		"'hello'",
		"['a', 1]",
		"{'low': 1, 'high': 2}",
		"('a', 'b')",
		"make_value()",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, expr string) {
		if len(expr) > 2048 {
			t.Skip("input too large")
		}

		source := []byte("VALUE = " + expr + "\n")
		tree, root := parseFuzzRoot(t, source)
		if root == nil {
			return
		}
		assign := findFirstNodeByType(root, "assignment")
		if assign == nil {
			return
		}
		right := assign.ChildByFieldName("right")
		if right == nil {
			return
		}

		_, _ = parseDefaultValue(right, source)
		scope := collectModuleScope(root, source)
		_, _ = resolveDefaultExpr(right, source, scope)
		_, _ = resolveStringExpr(right, source, scope)
		_, _ = resolveChoicesExpr(right, source, scope)
		runtime.KeepAlive(tree)
	})
}
