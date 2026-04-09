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
		info, err := os.Stat(fullPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", relPath, err)
		}

		source, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", relPath, err)
		}

		fixed := fixPydanticBaseModel(string(source))

		if err := os.WriteFile(fullPath, []byte(fixed), info.Mode()); err != nil {
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
// Uses tree-sitter to properly parse keyword arguments, avoiding false positives
// on arbitrary_types_allowed=False.
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
		if right == nil || right.Type() != "call" {
			continue
		}

		// Walk keyword arguments of the call
		args := right.ChildByFieldName("arguments")
		if args == nil {
			continue
		}
		for _, arg := range schemaPython.NamedChildren(args) {
			if arg.Type() != "keyword_argument" {
				continue
			}
			key := arg.ChildByFieldName("name")
			val := arg.ChildByFieldName("value")
			if key != nil && val != nil &&
				schemaPython.Content(key, source) == "arbitrary_types_allowed" &&
				schemaPython.Content(val, source) == "True" {
				return true
			}
		}
	}

	return false
}

// fixPydanticBaseModel rewrites Python source to replace pydantic.BaseModel with cog.BaseModel.
func fixPydanticBaseModel(source string) string {
	lines := strings.Split(source, "\n")
	var result []string
	inPydanticImport := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle multi-line "from pydantic import (" style
		if strings.HasPrefix(trimmed, "from pydantic import (") {
			inPydanticImport = true
			// Check if this single-line also closes: from pydantic import (BaseModel, ConfigDict)
			if strings.Contains(trimmed, ")") {
				inPydanticImport = false
				remaining := removePydanticImportsMultiline(trimmed)
				if remaining != "" {
					result = append(result, remaining)
				}
			}
			continue
		}
		if inPydanticImport {
			if strings.Contains(trimmed, ")") {
				inPydanticImport = false
			}
			// Skip all lines in the parenthesized pydantic import
			continue
		}

		// Remove single-line "from pydantic import BaseModel" (and ConfigDict)
		if strings.HasPrefix(trimmed, "from pydantic import") {
			remaining := removePydanticImports(trimmed)
			if remaining == "" {
				continue // Drop the entire line
			}
			result = append(result, remaining)
			continue
		}

		// Handle "import pydantic" style
		if trimmed == "import pydantic" {
			continue // Drop the line
		}

		// Handle model_config = ConfigDict(...) lines -- only remove arbitrary_types_allowed=True
		if strings.Contains(trimmed, "model_config") && strings.Contains(trimmed, "ConfigDict") {
			fixed := removeKeywordArg(line, "arbitrary_types_allowed=True")
			if fixed != line {
				// Successfully removed the argument; check if ConfigDict is now empty
				if isEmptyConfigDict(fixed) {
					continue // Drop the line entirely
				}
				result = append(result, fixed)
				continue
			}
		}

		// Replace pydantic.BaseModel in class definitions
		if strings.Contains(line, "pydantic.BaseModel") {
			line = strings.ReplaceAll(line, "pydantic.BaseModel", "BaseModel")
		}

		// Replace pydantic.ConfigDict references
		if strings.Contains(line, "pydantic.ConfigDict") {
			line = strings.ReplaceAll(line, "pydantic.ConfigDict", "ConfigDict")
		}

		result = append(result, line)
	}

	// Now add BaseModel to the cog import line
	fixed := strings.Join(result, "\n")
	fixed = addToCogImport(fixed, "BaseModel")

	return fixed
}

// removeKeywordArg removes a specific keyword argument from a line containing a function call.
// Handles "arg, ", ", arg", and standalone "arg".
func removeKeywordArg(line string, arg string) string {
	result := strings.Replace(line, arg+", ", "", 1)
	if result == line {
		result = strings.Replace(line, ", "+arg, "", 1)
	}
	if result == line {
		result = strings.Replace(line, arg, "", 1)
	}
	return result
}

// isEmptyConfigDict checks if a line contains an empty ConfigDict() call.
func isEmptyConfigDict(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Contains(trimmed, "ConfigDict()") || strings.Contains(trimmed, "ConfigDict( )")
}

// removePydanticImportsMultiline handles "from pydantic import (X, Y, Z)" on a single line.
func removePydanticImportsMultiline(line string) string {
	// Extract contents between "from pydantic import (" and ")"
	start := strings.Index(line, "(")
	end := strings.LastIndex(line, ")")
	if start == -1 || end == -1 || start >= end {
		return ""
	}
	importPart := line[start+1 : end]
	names := strings.Split(importPart, ",")

	var remaining []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "BaseModel" || name == "ConfigDict" || name == "" {
			continue
		}
		remaining = append(remaining, name)
	}

	if len(remaining) == 0 {
		return ""
	}

	return "from pydantic import " + strings.Join(remaining, ", ")
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
// If no "from cog import" line exists, inserts one at the top of the file.
func addToCogImport(source string, name string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "from cog import") {
			if strings.Contains(trimmed, name) {
				return source // Already imported
			}
			// Add the name at the end
			lines[i] = line + ", " + name
			return strings.Join(lines, "\n")
		}
	}
	// No existing cog import found -- add one at the top
	newImport := "from cog import " + name
	return newImport + "\n" + source
}
