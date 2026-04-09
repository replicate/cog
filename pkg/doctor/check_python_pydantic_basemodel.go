package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
	schemaPython "github.com/replicate/cog/pkg/schema/python"
)

// PydanticBaseModelCheck detects output classes that inherit from pydantic.BaseModel
// with arbitrary_types_allowed=True instead of using cog.BaseModel.
type PydanticBaseModelCheck struct{}

func (c *PydanticBaseModelCheck) Name() string        { return "python-pydantic-basemodel" }
func (c *PydanticBaseModelCheck) Group() Group        { return GroupPython }
func (c *PydanticBaseModelCheck) Description() string { return "Pydantic BaseModel workaround" }

func (c *PydanticBaseModelCheck) Check(ctx *CheckContext) ([]Finding, error) {
	var findings []Finding

	for _, pf := range ctx.PythonFiles {
		root := pf.Tree.RootNode()

		for _, child := range schemaPython.NamedChildren(root) {
			classNode := schemaPython.UnwrapClass(child)
			if classNode == nil {
				continue
			}

			// Check if class inherits from pydantic.BaseModel (not cog.BaseModel)
			if !inheritsPydanticBaseModel(classNode, pf.Source, pf.Imports) {
				continue
			}

			// Check if class has arbitrary_types_allowed=True
			if !hasArbitraryTypesAllowed(classNode, pf.Source) {
				continue
			}

			nameNode := classNode.ChildByFieldName("name")
			className := ""
			line := 0
			if nameNode != nil {
				className = schemaPython.Content(nameNode, pf.Source)
				line = int(nameNode.StartPoint().Row) + 1
			}

			findings = append(findings, Finding{
				Severity:    SeverityError,
				Message:     fmt.Sprintf("%s inherits from pydantic.BaseModel with arbitrary_types_allowed; use cog.BaseModel instead", className),
				Remediation: "Replace pydantic.BaseModel with cog.BaseModel and remove ConfigDict(arbitrary_types_allowed=True)",
				File:        pf.Path,
				Line:        line,
			})
		}
	}

	return findings, nil
}

func (c *PydanticBaseModelCheck) Fix(ctx *CheckContext, findings []Finding) error {
	// Group findings by file
	fileFindings := make(map[string][]Finding)
	for _, f := range findings {
		fileFindings[f.File] = append(fileFindings[f.File], f)
	}

	for relPath := range fileFindings {
		fullPath := filepath.Join(ctx.ProjectDir, relPath)
		source, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", relPath, err)
		}

		fixed := fixPydanticBaseModel(string(source))

		if err := os.WriteFile(fullPath, []byte(fixed), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	return nil
}

// inheritsPydanticBaseModel checks if a class inherits from pydantic.BaseModel
// (as opposed to cog.BaseModel or another BaseModel).
func inheritsPydanticBaseModel(classNode *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	supers := classNode.ChildByFieldName("superclasses")
	if supers == nil {
		return false
	}

	for _, child := range schemaPython.AllChildren(supers) {
		text := schemaPython.Content(child, source)

		switch child.Type() {
		case "identifier":
			if text == "BaseModel" {
				// Check if BaseModel was imported from pydantic
				if entry, ok := imports.Names.Get("BaseModel"); ok {
					return entry.Module == "pydantic"
				}
			}
		case "attribute":
			if text == "pydantic.BaseModel" {
				return true
			}
		}
	}
	return false
}

// hasArbitraryTypesAllowed checks if a class body contains
// model_config = ConfigDict(arbitrary_types_allowed=True).
func hasArbitraryTypesAllowed(classNode *sitter.Node, source []byte) bool {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return false
	}

	for _, stmt := range schemaPython.NamedChildren(body) {
		node := stmt
		if stmt.Type() == "expression_statement" && stmt.NamedChildCount() == 1 {
			node = stmt.NamedChild(0)
		}

		if node.Type() != "assignment" {
			continue
		}

		left := node.ChildByFieldName("left")
		if left == nil || schemaPython.Content(left, source) != "model_config" {
			continue
		}

		right := node.ChildByFieldName("right")
		if right == nil {
			continue
		}

		text := schemaPython.Content(right, source)
		if strings.Contains(text, "arbitrary_types_allowed") && strings.Contains(text, "True") {
			return true
		}
	}

	return false
}

// fixPydanticBaseModel rewrites Python source to replace pydantic.BaseModel with cog.BaseModel.
func fixPydanticBaseModel(source string) string {
	lines := strings.Split(source, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Remove "from pydantic import BaseModel" (and ConfigDict)
		if strings.HasPrefix(trimmed, "from pydantic import") {
			// Remove BaseModel and ConfigDict from the import
			remaining := removePydanticImports(trimmed)
			if remaining == "" {
				continue // Drop the entire line
			}
			result = append(result, remaining)
			continue
		}

		// Remove model_config = ConfigDict(...) lines
		if strings.Contains(trimmed, "model_config") && strings.Contains(trimmed, "ConfigDict") {
			continue
		}

		result = append(result, line)
	}

	// Now add BaseModel to the cog import line
	fixed := strings.Join(result, "\n")
	fixed = addToCogImport(fixed, "BaseModel")

	return fixed
}

// removePydanticImports removes BaseModel and ConfigDict from a pydantic import line.
// Returns "" if no imports remain.
func removePydanticImports(line string) string {
	// Parse "from pydantic import X, Y, Z"
	prefix := "from pydantic import "
	if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
		return line
	}

	importPart := strings.TrimSpace(line)[len(prefix):]
	names := strings.Split(importPart, ",")

	var remaining []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "BaseModel" || name == "ConfigDict" {
			continue
		}
		if name != "" {
			remaining = append(remaining, name)
		}
	}

	if len(remaining) == 0 {
		return ""
	}

	return prefix + strings.Join(remaining, ", ")
}

// addToCogImport adds a name to an existing "from cog import ..." line.
func addToCogImport(source string, name string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "from cog import") {
			if strings.Contains(trimmed, name) {
				return source // Already imported
			}
			// Add the name at the end
			lines[i] = trimmed + ", " + name
			return strings.Join(lines, "\n")
		}
	}
	return source
}
