package python

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

type moduleScope map[string]schema.DefaultValue

func collectModuleScope(root *sitter.Node, source []byte) moduleScope {
	scope := make(moduleScope)
	for _, child := range NamedChildren(root) {
		var assign *sitter.Node
		if child.Type() == "expression_statement" {
			if child.NamedChildCount() == 1 {
				inner := child.NamedChild(0)
				if inner.Type() == "assignment" {
					assign = inner
				}
			}
		} else if child.Type() == "assignment" {
			assign = child
		}
		if assign == nil {
			continue
		}

		left := assign.ChildByFieldName("left")
		if left == nil || left.Type() != "identifier" {
			continue
		}
		name := Content(left, source)

		right := assign.ChildByFieldName("right")
		if right == nil {
			continue
		}
		if val, ok := parseDefaultValue(right, source); ok {
			scope[name] = val
		}
	}
	return scope
}

func resolveStringExpr(node *sitter.Node, source []byte, scope moduleScope) (string, bool) {
	if s, ok := parseStringLiteral(node, source); ok {
		return s, true
	}
	if node.Type() == "identifier" {
		name := Content(node, source)
		if val, ok := scope[name]; ok && val.Kind == schema.DefaultString {
			return val.Str, true
		}
	}
	return "", false
}

func resolveDefaultExpr(node *sitter.Node, source []byte, scope moduleScope) (schema.DefaultValue, bool) {
	if val, ok := parseDefaultValue(node, source); ok {
		return val, true
	}
	if node.Type() == "identifier" {
		name := Content(node, source)
		if val, ok := scope[name]; ok {
			return val, true
		}
	}
	return schema.DefaultValue{}, false
}

func resolveChoicesExpr(node *sitter.Node, source []byte, scope moduleScope) ([]schema.DefaultValue, bool) {
	switch node.Type() {
	case "list":
		return parseListLiteral(node, source)
	case "identifier":
		name := Content(node, source)
		val, ok := scope[name]
		if !ok {
			return nil, false
		}
		if val.Kind == schema.DefaultList {
			return val.List, true
		}
		return nil, false
	case "call":
		return resolveChoicesCall(node, source, scope)
	case "binary_operator":
		hasPlus := false
		for _, c := range AllChildren(node) {
			if !c.IsNamed() && Content(c, source) == "+" {
				hasPlus = true
				break
			}
		}
		if !hasPlus {
			return nil, false
		}
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		if left == nil || right == nil {
			return nil, false
		}
		leftItems, ok := resolveChoicesExpr(left, source, scope)
		if !ok {
			return nil, false
		}
		rightItems, ok := resolveChoicesExpr(right, source, scope)
		if !ok {
			return nil, false
		}
		return append(leftItems, rightItems...), true
	}
	return nil, false
}

func resolveChoicesCall(node *sitter.Node, source []byte, scope moduleScope) ([]schema.DefaultValue, bool) {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil || Content(funcNode, source) != "list" {
		return nil, false
	}

	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil, false
	}

	var arg *sitter.Node
	for _, c := range NamedChildren(args) {
		arg = c
		break
	}
	if arg == nil || arg.Type() != "call" {
		return nil, false
	}

	innerFunc := arg.ChildByFieldName("function")
	if innerFunc == nil || innerFunc.Type() != "attribute" {
		return nil, false
	}

	obj := innerFunc.ChildByFieldName("object")
	attr := innerFunc.ChildByFieldName("attribute")
	if obj == nil || attr == nil || obj.Type() != "identifier" {
		return nil, false
	}

	varName := Content(obj, source)
	methodName := Content(attr, source)

	dictVal, ok := scope[varName]
	if !ok || dictVal.Kind != schema.DefaultDict {
		return nil, false
	}

	switch methodName {
	case "keys":
		return dictVal.DictKeys, true
	case "values":
		return dictVal.DictVals, true
	}
	return nil, false
}
