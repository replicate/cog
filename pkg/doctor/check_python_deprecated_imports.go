package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	schemaPython "github.com/replicate/cog/pkg/schema/python"
)

// deprecatedImport describes an import that was removed or moved.
type deprecatedImport struct {
	Module  string // e.g., "cog.types"
	Name    string // e.g., "ExperimentalFeatureWarning"
	Message string // e.g., "removed in cog 0.17"
}

// deprecatedImportsList is the list of known deprecated imports.
var deprecatedImportsList = []deprecatedImport{
	{
		Module:  "cog.types",
		Name:    "ExperimentalFeatureWarning",
		Message: "ExperimentalFeatureWarning was removed in cog 0.17; current_scope().record_metric() is no longer experimental",
	},
}

// DeprecatedImportsCheck detects imports that were removed or moved in recent cog versions.
type DeprecatedImportsCheck struct{}

func (c *DeprecatedImportsCheck) Name() string        { return "python-deprecated-imports" }
func (c *DeprecatedImportsCheck) Group() Group        { return GroupPython }
func (c *DeprecatedImportsCheck) Description() string { return "Deprecated imports" }

func (c *DeprecatedImportsCheck) Check(ctx *CheckContext) ([]Finding, error) {
	var findings []Finding

	for _, pf := range ctx.PythonFiles {
		root := pf.Tree.RootNode()

		for _, child := range schemaPython.NamedChildren(root) {
			if child.Type() != "import_from_statement" {
				continue
			}

			moduleNode := child.ChildByFieldName("module_name")
			if moduleNode == nil {
				continue
			}
			module := schemaPython.Content(moduleNode, pf.Source)

			// Check each imported name against the deprecated list
			for _, name := range extractImportedNames(child, pf.Source) {
				for _, dep := range deprecatedImportsList {
					if module == dep.Module && name == dep.Name {
						line := int(child.StartPoint().Row) + 1
						findings = append(findings, Finding{
							Severity:    SeverityError,
							Message:     dep.Message,
							Remediation: fmt.Sprintf("Remove the import of %s from %s", dep.Name, dep.Module),
							File:        pf.Path,
							Line:        line,
						})
					}
				}
			}
		}
	}

	return findings, nil
}

func (c *DeprecatedImportsCheck) Fix(ctx *CheckContext, findings []Finding) error {
	// Group findings by file
	fileFindings := make(map[string][]Finding)
	for _, f := range findings {
		fileFindings[f.File] = append(fileFindings[f.File], f)
	}

	for relPath, fileFindingsList := range fileFindings {
		fullPath := filepath.Join(ctx.ProjectDir, relPath)
		source, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", relPath, err)
		}

		// Collect deprecated names to remove
		namesToRemove := make(map[string]map[string]bool) // module -> set of names
		for _, f := range fileFindingsList {
			for _, dep := range deprecatedImportsList {
				if strings.Contains(f.Message, dep.Name) {
					if namesToRemove[dep.Module] == nil {
						namesToRemove[dep.Module] = make(map[string]bool)
					}
					namesToRemove[dep.Module][dep.Name] = true
				}
			}
		}

		fixed := removeDeprecatedImportLines(string(source), namesToRemove)

		if err := os.WriteFile(fullPath, []byte(fixed), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	return nil
}

// extractImportedNames returns the names imported in a "from X import a, b, c" statement.
func extractImportedNames(importNode *sitter.Node, source []byte) []string {
	moduleNode := importNode.ChildByFieldName("module_name")
	var names []string

	for _, child := range schemaPython.AllChildren(importNode) {
		switch child.Type() {
		case "dotted_name":
			if moduleNode != nil && child.StartByte() != moduleNode.StartByte() {
				names = append(names, schemaPython.Content(child, source))
			}
		case "aliased_import":
			if origNode := child.ChildByFieldName("name"); origNode != nil {
				names = append(names, schemaPython.Content(origNode, source))
			}
		case "import_list":
			for _, ic := range schemaPython.AllChildren(child) {
				switch ic.Type() {
				case "dotted_name":
					names = append(names, schemaPython.Content(ic, source))
				case "aliased_import":
					if origNode := ic.ChildByFieldName("name"); origNode != nil {
						names = append(names, schemaPython.Content(origNode, source))
					}
				}
			}
		}
	}

	return names
}

// removeDeprecatedImportLines removes specific names from import lines.
// If all names are removed, the entire import line is dropped.
func removeDeprecatedImportLines(source string, namesToRemove map[string]map[string]bool) string {
	lines := strings.Split(source, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		removed := false
		for module, names := range namesToRemove {
			prefix := "from " + module + " import "
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}

			importPart := trimmed[len(prefix):]
			importNames := strings.Split(importPart, ",")

			var remaining []string
			for _, name := range importNames {
				name = strings.TrimSpace(name)
				if !names[name] {
					remaining = append(remaining, name)
				}
			}

			if len(remaining) == 0 {
				removed = true
			} else {
				result = append(result, prefix+strings.Join(remaining, ", "))
				removed = true
			}
			break
		}

		if !removed {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}
