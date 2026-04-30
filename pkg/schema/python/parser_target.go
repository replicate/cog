package python

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

func findTargetFunction(facts moduleFacts, source []byte, predictRef, methodName string) (*sitter.Node, error) {
	for _, classDef := range facts.classes {
		if classDef.name == predictRef {
			return findMethodInClass(classDef.node, source, predictRef, methodName)
		}
	}

	for _, functionDef := range facts.functions {
		if functionDef.name == predictRef || functionDef.name == methodName {
			return functionDef.node, nil
		}
	}

	return nil, schema.WrapError(schema.ErrPredictorNotFound, predictRef, nil)
}

func findMethodInClass(classNode *sitter.Node, source []byte, className, methodName string) (*sitter.Node, error) {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil, schema.WrapError(schema.ErrParse, fmt.Sprintf("class %s has no body", className), nil)
	}

	for _, child := range NamedChildren(body) {
		funcNode := UnwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode != nil && Content(nameNode, source) == methodName {
			return funcNode, nil
		}
	}

	return nil, schema.WrapError(schema.ErrMethodNotFound, fmt.Sprintf("%s.%s not found", className, methodName), nil)
}

func firstParamIsSelf(params *sitter.Node, source []byte) bool {
	for _, child := range AllChildren(params) {
		if child.Type() == "identifier" {
			return Content(child, source) == "self"
		}
	}
	return false
}
