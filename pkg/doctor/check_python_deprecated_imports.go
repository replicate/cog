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
	affectedFiles := make(map[string]bool)
	for _, f := range findings {
		affectedFiles[f.File] = true
	}

	for relPath := range affectedFiles {
		fullPath := filepath.Join(ctx.ProjectDir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", relPath, err)
		}

		source, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", relPath, err)
		}

		// Re-scan the file using tree-sitter to find deprecated imports directly,
		// rather than relying on fragile string matching against finding messages.
		pf, ok := ctx.PythonFiles[relPath]
		if !ok {
			continue
		}
		namesToRemove := make(map[string]map[string]bool) // module -> set of names
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
			for _, name := range extractImportedNames(child, pf.Source) {
				for _, dep := range deprecatedImportsList {
					if module == dep.Module && name == dep.Name {
						if namesToRemove[dep.Module] == nil {
							namesToRemove[dep.Module] = make(map[string]bool)
						}
						namesToRemove[dep.Module][dep.Name] = true
					}
				}
			}
		}

		fixed := removeDeprecatedImportLines(string(source), namesToRemove)

		if err := os.WriteFile(fullPath, []byte(fixed), info.Mode()); err != nil {
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
// Handles both single-line and multi-line parenthesized imports.
func removeDeprecatedImportLines(source string, namesToRemove map[string]map[string]bool) string {
	lines := strings.Split(source, "\n")
	var result []string

	// Track multi-line import state
	inMultilineImport := false
	var multilineModule string
	var multilineNames []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle multi-line imports
		if inMultilineImport {
			// Collect names from continuation lines
			cleaned := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed), ")"))
			if cleaned != "" {
				for n := range strings.SplitSeq(cleaned, ",") {
					n = strings.TrimSpace(n)
					if n != "" {
						multilineNames = append(multilineNames, n)
					}
				}
			}
			if strings.Contains(trimmed, ")") {
				inMultilineImport = false
				// Now filter the collected names
				names := namesToRemove[multilineModule]
				var remaining []string
				for _, name := range multilineNames {
					if !names[name] {
						remaining = append(remaining, name)
					}
				}
				if len(remaining) > 0 {
					result = append(result, "from "+multilineModule+" import "+strings.Join(remaining, ", "))
				}
			}
			continue
		}

		removed := false
		for module, names := range namesToRemove {
			prefix := "from " + module + " import "
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}

			importPart := trimmed[len(prefix):]

			// Check for multi-line parenthesized import
			if strings.HasPrefix(strings.TrimSpace(importPart), "(") {
				inner := strings.TrimSpace(importPart)[1:] // strip leading "("
				if strings.Contains(inner, ")") {
					// Single-line parenthesized: from X import (A, B)
					inner = strings.TrimSuffix(strings.TrimSpace(inner), ")")
					importNames := strings.Split(inner, ",")
					var remaining []string
					for _, name := range importNames {
						name = strings.TrimSpace(name)
						if name != "" && !names[name] {
							remaining = append(remaining, name)
						}
					}
					if len(remaining) > 0 {
						result = append(result, prefix+strings.Join(remaining, ", "))
					}
					removed = true
				} else {
					// Start of multi-line import
					inMultilineImport = true
					multilineModule = module
					multilineNames = nil
					// Collect any names on the first line after "("
					if inner != "" {
						for n := range strings.SplitSeq(inner, ",") {
							n = strings.TrimSpace(n)
							if n != "" {
								multilineNames = append(multilineNames, n)
							}
						}
					}
					removed = true
				}
				break
			}

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
