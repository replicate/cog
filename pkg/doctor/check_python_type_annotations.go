package doctor

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"

	schemaPython "github.com/replicate/cog/pkg/schema/python"
)

// MissingTypeAnnotationsCheck detects predict/train methods that are
// missing return type annotations.
type MissingTypeAnnotationsCheck struct{}

func (c *MissingTypeAnnotationsCheck) Name() string        { return "python-missing-type-annotations" }
func (c *MissingTypeAnnotationsCheck) Group() Group        { return GroupPython }
func (c *MissingTypeAnnotationsCheck) Description() string { return "Type annotations" }

func (c *MissingTypeAnnotationsCheck) Check(ctx *CheckContext) ([]Finding, error) {
	if ctx.Config == nil {
		return nil, nil
	}

	var findings []Finding

	if ctx.Config.Predict != "" {
		f := checkMethodAnnotations(ctx, ctx.Config.Predict, "predict")
		findings = append(findings, f...)
	}

	if ctx.Config.Train != "" {
		f := checkMethodAnnotations(ctx, ctx.Config.Train, "train")
		findings = append(findings, f...)
	}

	return findings, nil
}

func (c *MissingTypeAnnotationsCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}

// checkMethodAnnotations checks that the given method has a return type annotation.
func checkMethodAnnotations(ctx *CheckContext, ref string, methodName string) []Finding {
	fileName, className := splitPredictRef(ref)

	if fileName == "" || className == "" {
		return nil // Invalid ref — handled by ConfigPredictRefCheck
	}

	pf, ok := ctx.PythonFiles[fileName]
	if !ok {
		return nil // File not parsed — handled by ConfigPredictRefCheck
	}

	root := pf.Tree.RootNode()

	// Find the class
	classNode := findClass(root, pf.Source, className)
	if classNode == nil {
		return nil // Class not found — handled by ConfigPredictRefCheck
	}

	// Find the method inside the class
	funcNode := findMethod(classNode, pf.Source, methodName)
	if funcNode == nil {
		return nil // Method not found — could be a separate check later
	}

	// Check for return type annotation
	if funcNode.ChildByFieldName("return_type") == nil {
		line := int(funcNode.StartPoint().Row) + 1
		return []Finding{{
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"%s.%s() is missing a return type annotation",
				className, methodName,
			),
			Remediation: fmt.Sprintf("Add a return type annotation: def %s(self, ...) -> YourType:", methodName),
			File:        fileName,
			Line:        line,
		}}
	}

	return nil
}

// findClass locates a top-level class by name.
func findClass(root *sitter.Node, source []byte, name string) *sitter.Node {
	for _, child := range schemaPython.NamedChildren(root) {
		classNode := schemaPython.UnwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode != nil && schemaPython.Content(nameNode, source) == name {
			return classNode
		}
	}
	return nil
}

// findMethod locates a method by name inside a class body.
func findMethod(classNode *sitter.Node, source []byte, name string) *sitter.Node {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	for _, child := range schemaPython.NamedChildren(body) {
		funcNode := schemaPython.UnwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode != nil && schemaPython.Content(nameNode, source) == name {
			return funcNode
		}
	}
	return nil
}
