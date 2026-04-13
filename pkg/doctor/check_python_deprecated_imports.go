package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

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

		pf, ok := ctx.PythonFiles[relPath]
		if !ok {
			continue
		}

		fixed := removeDeprecatedImportsAST(source, pf.Tree)

		if err := os.WriteFile(fullPath, []byte(fixed), info.Mode()); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	return nil
}

// byteRange represents a range of bytes to remove from source, corresponding
// to a full line (including its trailing newline).
type byteRange struct {
	start uint32
	end   uint32
}

// removeDeprecatedImportsAST uses tree-sitter to identify and remove:
// 1. import_from_statement nodes that import deprecated names
// 2. expression_statement nodes that reference those deprecated names
// 3. orphaned "import X" statements where X is no longer used
func removeDeprecatedImportsAST(source []byte, tree *sitter.Tree) string {
	root := tree.RootNode()

	// Step 1: Walk the AST to find which deprecated names are present in this file.
	deprecatedNames := make(map[string]bool)
	namesToRemove := make(map[string]map[string]bool) // module -> set of names

	for _, child := range schemaPython.NamedChildren(root) {
		if child.Type() != "import_from_statement" {
			continue
		}
		moduleNode := child.ChildByFieldName("module_name")
		if moduleNode == nil {
			continue
		}
		module := schemaPython.Content(moduleNode, source)

		for _, name := range extractImportedNames(child, source) {
			for _, dep := range deprecatedImportsList {
				if module == dep.Module && name == dep.Name {
					deprecatedNames[dep.Name] = true
					if namesToRemove[dep.Module] == nil {
						namesToRemove[dep.Module] = make(map[string]bool)
					}
					namesToRemove[dep.Module][dep.Name] = true
				}
			}
		}
	}

	if len(deprecatedNames) == 0 {
		return string(source)
	}

	// Step 2: Remove deprecated import lines/names (handles partial imports).
	fixed := removeDeprecatedImportLines(string(source), namesToRemove)

	// Step 3: Re-parse and use tree-sitter to find statements referencing
	// the deprecated names, then remove them by byte range.
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	newTree, err := parser.ParseCtx(context.Background(), nil, []byte(fixed))
	if err != nil {
		return fixed
	}

	newSource := []byte(fixed)
	newRoot := newTree.RootNode()
	var removals []byteRange
	for _, child := range schemaPython.NamedChildren(newRoot) {
		if child.Type() == "import_from_statement" || child.Type() == "import_statement" {
			continue
		}
		if nodeReferencesAny(child, newSource, deprecatedNames) {
			removals = append(removals, nodeLineRange(child, newSource))
		}
	}

	fixed = applyRemovals(newSource, removals)

	// Step 4: Remove orphaned "import X" statements via AST.
	fixed = removeOrphanedImportsAST(fixed)

	return fixed
}

// nodeReferencesAny walks a tree-sitter node recursively and returns true if
// any identifier node matches one of the given names.
func nodeReferencesAny(node *sitter.Node, source []byte, names map[string]bool) bool {
	if node.Type() == "identifier" {
		return names[schemaPython.Content(node, source)]
	}
	for _, child := range schemaPython.AllChildren(node) {
		if nodeReferencesAny(child, source, names) {
			return true
		}
	}
	return false
}

// nodeLineRange returns a byte range covering the full line(s) of a node,
// including the trailing newline.
func nodeLineRange(node *sitter.Node, source []byte) byteRange {
	start := node.StartByte()
	end := node.EndByte()

	// Extend start to beginning of line
	for start > 0 && source[start-1] != '\n' {
		start--
	}
	// Extend end past trailing newline
	if int(end) < len(source) && source[end] == '\n' {
		end++
	}

	return byteRange{start: start, end: end}
}

// applyRemovals removes all byte ranges from source, handling overlaps.
// Ranges are sorted descending by start so earlier indices remain valid.
func applyRemovals(source []byte, ranges []byteRange) string {
	if len(ranges) == 0 {
		return string(source)
	}

	// Sort descending by start so we can remove from back to front
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start > ranges[j].start
	})

	result := make([]byte, len(source))
	copy(result, source)

	for _, r := range ranges {
		if int(r.start) >= len(result) {
			continue
		}
		end := min(int(r.end), len(result))
		result = append(result[:r.start], result[end:]...)
	}

	return string(result)
}

// removeOrphanedImportsAST re-parses source and removes "import X" statements
// where X is no longer referenced anywhere else in the file.
func removeOrphanedImportsAST(source string) string {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, []byte(source))
	if err != nil {
		return source
	}

	src := []byte(source)
	root := tree.RootNode()
	var removals []byteRange

	for _, child := range schemaPython.NamedChildren(root) {
		if child.Type() != "import_statement" {
			continue
		}

		// Get the module name being imported (e.g. "warnings" from "import warnings")
		var moduleName string
		for _, c := range schemaPython.NamedChildren(child) {
			if c.Type() == "dotted_name" {
				moduleName = schemaPython.Content(c, src)
				break
			}
		}
		if moduleName == "" {
			continue
		}

		// Check if this module is referenced elsewhere (outside import statements)
		used := false
		for _, stmt := range schemaPython.NamedChildren(root) {
			if stmt.Type() == "import_statement" || stmt.Type() == "import_from_statement" {
				continue
			}
			if nodeReferencesModule(stmt, src, moduleName) {
				used = true
				break
			}
		}

		if !used {
			removals = append(removals, nodeLineRange(child, src))
		}
	}

	return applyRemovals(src, removals)
}

// nodeReferencesModule checks if a node contains an attribute access on the
// given module (e.g. "warnings.filterwarnings") or a bare identifier matching it.
func nodeReferencesModule(node *sitter.Node, source []byte, moduleName string) bool {
	if node.Type() == "attribute" {
		obj := node.ChildByFieldName("object")
		if obj != nil && obj.Type() == "identifier" && schemaPython.Content(obj, source) == moduleName {
			return true
		}
	}
	if node.Type() == "identifier" && schemaPython.Content(node, source) == moduleName {
		return true
	}
	for _, child := range schemaPython.AllChildren(node) {
		if nodeReferencesModule(child, source, moduleName) {
			return true
		}
	}
	return false
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
