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

	// Iterate in sorted order so findings are deterministic.
	paths := make([]string, 0, len(ctx.PythonFiles))
	for p := range ctx.PythonFiles {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		pf := ctx.PythonFiles[path]
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

	// Iterate in sorted order so errors/writes are deterministic.
	relPaths := make([]string, 0, len(fileFindings))
	for p := range fileFindings {
		relPaths = append(relPaths, p)
	}
	sort.Strings(relPaths)

	for _, relPath := range relPaths {
		fullPath := filepath.Join(ctx.ProjectDir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", relPath, err)
		}

		source, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", relPath, err)
		}

		fixed, err := fixPydanticBaseModel(ctx.ctx, source)
		if err != nil {
			return fmt.Errorf("fixing %s: %w", relPath, err)
		}

		if err := os.WriteFile(fullPath, fixed, info.Mode()); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	return nil
}

// inheritsPydanticBaseModel checks if a class inherits from pydantic.BaseModel
// (as opposed to cog.BaseModel or another BaseModel). Handles:
//   - class X(BaseModel)       with  from pydantic import BaseModel
//   - class X(PBM)             with  from pydantic import BaseModel as PBM
//   - class X(pydantic.BaseModel)
func inheritsPydanticBaseModel(classNode *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	supers := classNode.ChildByFieldName("superclasses")
	if supers == nil {
		return false
	}

	for _, child := range schemaPython.AllChildren(supers) {
		switch child.Type() {
		case "identifier":
			// Look up whatever local identifier is used, then check whether it
			// resolves to pydantic.BaseModel — this catches aliased imports
			// like `from pydantic import BaseModel as PBM` where the super is `PBM`.
			name := schemaPython.Content(child, source)
			if entry, ok := imports.Names.Get(name); ok {
				if entry.Module == "pydantic" && entry.Original == "BaseModel" {
					return true
				}
			}
		case "attribute":
			// Handle explicit `pydantic.BaseModel`. (Aliased module imports like
			// `import pydantic as pd` are not currently tracked by CollectImports,
			// so `pd.BaseModel` is not detected. Users using this pattern can
			// migrate manually; the check won't produce a false positive.)
			obj := child.ChildByFieldName("object")
			attr := child.ChildByFieldName("attribute")
			if obj == nil || attr == nil {
				continue
			}
			if schemaPython.Content(attr, source) != "BaseModel" {
				continue
			}
			if schemaPython.Content(obj, source) == "pydantic" {
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
		// A class body statement may be either an expression_statement wrapping
		// an assignment (the common case in tree-sitter-python) or an assignment
		// node directly. Handle both so we don't silently skip legitimate
		// model_config lines.
		var node *sitter.Node
		switch {
		case stmt.Type() == "assignment":
			node = stmt
		case stmt.Type() == "expression_statement" && stmt.NamedChildCount() == 1:
			node = stmt.NamedChild(0)
		default:
			continue
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

// byteEdit represents a single byte-range edit to a source buffer.
// If Replacement is empty, it is a pure deletion.
type byteEdit struct {
	start       uint32
	end         uint32
	replacement []byte
}

// fixPydanticBaseModel rewrites Python source to replace pydantic.BaseModel
// with cog.BaseModel for the specific classes that would trigger the check
// (inherits pydantic.BaseModel AND has arbitrary_types_allowed=True).
// Unrelated pydantic classes in the file are left untouched.
//
// If any unrelated class still references pydantic's BaseModel by its bare
// name (`class X(BaseModel):`), the fixer uses an aliased import
// (`from cog import BaseModel as CogBaseModel`) to avoid a name collision
// with the pydantic import. Otherwise it uses the plain `BaseModel` name.
func fixPydanticBaseModel(ctx context.Context, source []byte) ([]byte, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	defer tree.Close()
	root := tree.RootNode()
	imports := schemaPython.CollectImports(root, source)

	// Collect the flagged classes first so we can decide on the target alias.
	var flaggedClasses []*sitter.Node
	for _, child := range schemaPython.NamedChildren(root) {
		classNode := schemaPython.UnwrapClass(child)
		if classNode == nil {
			continue
		}
		if !inheritsPydanticBaseModel(classNode, source, imports) {
			continue
		}
		if !hasArbitraryTypesAllowed(classNode, source) {
			continue
		}
		flaggedClasses = append(flaggedClasses, classNode)
	}
	if len(flaggedClasses) == 0 {
		return source, nil
	}

	// Determine whether any *non-flagged* class still needs the pydantic
	// BaseModel / ConfigDict symbol. If so, we cannot drop them from the
	// pydantic import and must use aliased cog symbols to avoid collisions.
	collideBaseModel := anyNonFlaggedClassReferencesUnqualified(root, source, "BaseModel", imports, flaggedClasses)
	collideConfigDict := anyNonFlaggedReferenceOutsideImports(root, source, "ConfigDict", imports, flaggedClasses)

	cogBaseModelName := "BaseModel"
	if collideBaseModel {
		cogBaseModelName = "CogBaseModel"
	}

	var edits []byteEdit
	for _, classNode := range flaggedClasses {
		// 1. Rewrite the superclass reference so the class inherits cog.BaseModel
		//    (or the aliased form, if the collision check triggered).
		if e, ok := editSuperclassTo(classNode, source, imports, cogBaseModelName); ok {
			edits = append(edits, e)
		}
		// 2. Handle model_config = ConfigDict(...) assignments in the class body.
		edits = append(edits, editsForModelConfig(classNode, source)...)
	}

	// Apply intra-class edits.
	intermediate := applyEdits(source, edits)

	// Re-parse to do a clean pass over imports.
	tree2, err := parser.ParseCtx(ctx, nil, intermediate)
	if err != nil {
		return nil, fmt.Errorf("reparse: %w", err)
	}
	defer tree2.Close()
	root2 := tree2.RootNode()

	var edits2 []byteEdit
	usedPydanticModule := hasRemainingPydanticModuleRefs(root2, intermediate)

	// Determine which names to drop from `from pydantic import ...`.
	namesToDrop := map[string]bool{}
	if !collideBaseModel {
		namesToDrop["BaseModel"] = true
	}
	if !collideConfigDict {
		namesToDrop["ConfigDict"] = true
	}

	for _, child := range schemaPython.NamedChildren(root2) {
		if child.Type() != "import_from_statement" {
			continue
		}
		moduleNode := child.ChildByFieldName("module_name")
		if moduleNode == nil || schemaPython.Content(moduleNode, intermediate) != "pydantic" {
			continue
		}
		if e, ok := editDropImportedNames(child, intermediate, namesToDrop); ok {
			edits2 = append(edits2, e)
		}
	}

	// Drop bare `import pydantic` if no pydantic.* attribute access remains.
	if !usedPydanticModule {
		for _, child := range schemaPython.NamedChildren(root2) {
			if child.Type() != "import_statement" {
				continue
			}
			if importStatementImportsOnlyModule(child, intermediate, "pydantic") {
				edits2 = append(edits2, byteEdit{
					start: child.StartByte(),
					end:   endOfLine(intermediate, child.EndByte()),
				})
			}
		}
	}

	intermediate2 := applyEdits(intermediate, edits2)

	// Ensure the cog import supplies the symbol we just used.
	final := addToCogImport(ctx, intermediate2, cogBaseModelName)

	return final, nil
}

// anyNonFlaggedClassReferencesUnqualified returns true if any top-level class
// that is NOT in the flagged set has a bare `name` in its superclass list,
// where that `name` was imported from `module`.
func anyNonFlaggedClassReferencesUnqualified(root *sitter.Node, source []byte, name string, imports *schema.ImportContext, flagged []*sitter.Node) bool {
	entry, ok := imports.Names.Get(name)
	if !ok || entry.Module != "pydantic" || entry.Original != name {
		// The name wasn't imported from pydantic as itself, no collision.
		return false
	}
	flaggedSet := make(map[uint32]bool, len(flagged))
	for _, c := range flagged {
		flaggedSet[c.StartByte()] = true
	}
	for _, child := range schemaPython.NamedChildren(root) {
		classNode := schemaPython.UnwrapClass(child)
		if classNode == nil {
			continue
		}
		if flaggedSet[classNode.StartByte()] {
			continue
		}
		supers := classNode.ChildByFieldName("superclasses")
		if supers == nil {
			continue
		}
		for _, sc := range schemaPython.AllChildren(supers) {
			if sc.Type() == "identifier" && schemaPython.Content(sc, source) == name {
				return true
			}
		}
	}
	return false
}

// anyNonFlaggedReferenceOutsideImports returns true if the bare identifier
// `name` (imported from `module`) is referenced anywhere outside of import
// statements AND outside of any class body in `flagged`.
func anyNonFlaggedReferenceOutsideImports(root *sitter.Node, source []byte, name string, imports *schema.ImportContext, flagged []*sitter.Node) bool {
	entry, ok := imports.Names.Get(name)
	if !ok || entry.Module != "pydantic" || entry.Original != name {
		return false
	}
	flaggedSet := make(map[uint32]bool, len(flagged))
	for _, c := range flagged {
		flaggedSet[c.StartByte()] = true
	}
	return walkFindIdentifierOutsideImportsExcept(root, source, name, flaggedSet)
}

func walkFindIdentifierOutsideImportsExcept(node *sitter.Node, source []byte, name string, except map[uint32]bool) bool {
	switch node.Type() {
	case "import_statement", "import_from_statement":
		return false
	case "class_definition":
		if except[node.StartByte()] {
			return false
		}
	case "identifier":
		return schemaPython.Content(node, source) == name
	}
	for _, child := range schemaPython.AllChildren(node) {
		if walkFindIdentifierOutsideImportsExcept(child, source, name, except) {
			return true
		}
	}
	return false
}

// editSuperclassTo returns an edit that rewrites the class's pydantic-derived
// superclass to the given target name, for whichever form is used
// (`BaseModel`, `PBM`, `pydantic.BaseModel`).
func editSuperclassTo(classNode *sitter.Node, source []byte, imports *schema.ImportContext, target string) (byteEdit, bool) {
	supers := classNode.ChildByFieldName("superclasses")
	if supers == nil {
		return byteEdit{}, false
	}
	for _, child := range schemaPython.AllChildren(supers) {
		switch child.Type() {
		case "identifier":
			name := schemaPython.Content(child, source)
			if entry, ok := imports.Names.Get(name); ok &&
				entry.Module == "pydantic" && entry.Original == "BaseModel" {
				if name == target {
					return byteEdit{}, false // already correct spelling
				}
				return byteEdit{
					start:       child.StartByte(),
					end:         child.EndByte(),
					replacement: []byte(target),
				}, true
			}
		case "attribute":
			obj := child.ChildByFieldName("object")
			attr := child.ChildByFieldName("attribute")
			if obj == nil || attr == nil {
				continue
			}
			if schemaPython.Content(attr, source) == "BaseModel" &&
				schemaPython.Content(obj, source) == "pydantic" {
				return byteEdit{
					start:       child.StartByte(),
					end:         child.EndByte(),
					replacement: []byte(target),
				}, true
			}
		}
	}
	return byteEdit{}, false
}

// editsForModelConfig returns edits that:
//   - remove an `arbitrary_types_allowed=True` keyword argument from any
//     `model_config = ConfigDict(...)` / `model_config = pydantic.ConfigDict(...)`
//     assignment in the class body, and
//   - remove the entire assignment line if the resulting call has no arguments.
//
// It also rewrites `pydantic.ConfigDict` → `ConfigDict` for the assignments it touches.
func editsForModelConfig(classNode *sitter.Node, source []byte) []byteEdit {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	var edits []byteEdit
	for _, stmt := range schemaPython.NamedChildren(body) {
		var node *sitter.Node
		switch {
		case stmt.Type() == "assignment":
			node = stmt
		case stmt.Type() == "expression_statement" && stmt.NamedChildCount() == 1:
			node = stmt.NamedChild(0)
		default:
			continue
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
		fn := right.ChildByFieldName("function")
		args := right.ChildByFieldName("arguments")
		if fn == nil || args == nil {
			continue
		}
		// Confirm the call is ConfigDict or pydantic.ConfigDict.
		var fnEdit *byteEdit
		switch fn.Type() {
		case "identifier":
			if schemaPython.Content(fn, source) != "ConfigDict" {
				continue
			}
		case "attribute":
			attr := fn.ChildByFieldName("attribute")
			obj := fn.ChildByFieldName("object")
			if attr == nil || obj == nil {
				continue
			}
			if schemaPython.Content(attr, source) != "ConfigDict" ||
				schemaPython.Content(obj, source) != "pydantic" {
				continue
			}
			// Rewrite pydantic.ConfigDict → ConfigDict
			fnEdit = &byteEdit{
				start:       fn.StartByte(),
				end:         fn.EndByte(),
				replacement: []byte("ConfigDict"),
			}
		default:
			continue
		}

		// Find the arbitrary_types_allowed=True keyword arg. Count other args.
		var argToRemove *sitter.Node
		kwArgs := 0
		for _, arg := range schemaPython.NamedChildren(args) {
			if arg.Type() != "keyword_argument" {
				kwArgs++
				continue
			}
			key := arg.ChildByFieldName("name")
			val := arg.ChildByFieldName("value")
			if key != nil && val != nil &&
				schemaPython.Content(key, source) == "arbitrary_types_allowed" &&
				schemaPython.Content(val, source) == "True" {
				argToRemove = arg
				continue
			}
			kwArgs++
		}
		if argToRemove == nil {
			// Still emit the pydantic.ConfigDict rewrite if any.
			if fnEdit != nil {
				edits = append(edits, *fnEdit)
			}
			continue
		}

		if kwArgs == 0 {
			// No other args — remove the whole assignment statement line(s).
			edits = append(edits, byteEdit{
				start: startOfLine(source, stmt.StartByte()),
				end:   endOfLine(source, stmt.EndByte()),
			})
			continue
		}

		// Otherwise remove just the keyword arg + adjacent comma & whitespace.
		edits = append(edits, removeArgEdit(argToRemove, args, source))
		if fnEdit != nil {
			edits = append(edits, *fnEdit)
		}
	}
	return edits
}

// removeArgEdit returns a byte-range edit that removes a single argument from
// an argument list, including its adjacent comma and whitespace.
func removeArgEdit(arg, args *sitter.Node, source []byte) byteEdit {
	start := arg.StartByte()
	end := arg.EndByte()

	// Extend `end` forward past whitespace and a trailing comma.
	for int(end) < len(source) && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	if int(end) < len(source) && source[end] == ',' {
		end++
		// Consume following whitespace too.
		for int(end) < len(source) && (source[end] == ' ' || source[end] == '\t') {
			end++
		}
		return byteEdit{start: start, end: end}
	}
	// No trailing comma — look backward for a leading comma to remove instead.
	// Don't cross the opening paren of `args`.
	limit := args.StartByte() + 1
	for start > limit && (source[start-1] == ' ' || source[start-1] == '\t') {
		start--
	}
	if start > limit && source[start-1] == ',' {
		start--
		for start > limit && (source[start-1] == ' ' || source[start-1] == '\t') {
			start--
		}
	}
	return byteEdit{start: start, end: end}
}

// hasRemainingPydanticModuleRefs returns true if any attribute access of the
// form `pydantic.<anything>` remains in the tree.
func hasRemainingPydanticModuleRefs(root *sitter.Node, source []byte) bool {
	return walkFindAttribute(root, source, "pydantic")
}

func walkFindAttribute(node *sitter.Node, source []byte, objName string) bool {
	if node.Type() == "attribute" {
		obj := node.ChildByFieldName("object")
		if obj != nil && obj.Type() == "identifier" &&
			schemaPython.Content(obj, source) == objName {
			return true
		}
	}
	for _, child := range schemaPython.AllChildren(node) {
		if walkFindAttribute(child, source, objName) {
			return true
		}
	}
	return false
}

// editDropImportedNames returns an edit that rewrites a `from M import ...`
// statement to drop the named imports, or removes the line entirely if no
// names remain.
func editDropImportedNames(importNode *sitter.Node, source []byte, namesToDrop map[string]bool) (byteEdit, bool) {
	// Collect remaining imported names (preserving any aliases).
	type entry struct {
		text string // original source text for the name (possibly aliased)
		base string // base (original) name
	}
	var all []entry
	moduleNode := importNode.ChildByFieldName("module_name")
	for _, child := range schemaPython.AllChildren(importNode) {
		switch child.Type() {
		case "dotted_name":
			if moduleNode != nil && child.StartByte() == moduleNode.StartByte() {
				continue
			}
			n := schemaPython.Content(child, source)
			all = append(all, entry{text: n, base: n})
		case "aliased_import":
			orig := child.ChildByFieldName("name")
			if orig == nil {
				continue
			}
			all = append(all, entry{
				text: schemaPython.Content(child, source),
				base: schemaPython.Content(orig, source),
			})
		case "import_list":
			for _, ic := range schemaPython.AllChildren(child) {
				switch ic.Type() {
				case "dotted_name":
					n := schemaPython.Content(ic, source)
					all = append(all, entry{text: n, base: n})
				case "aliased_import":
					orig := ic.ChildByFieldName("name")
					if orig == nil {
						continue
					}
					all = append(all, entry{
						text: schemaPython.Content(ic, source),
						base: schemaPython.Content(orig, source),
					})
				}
			}
		}
	}

	var kept []string
	dropped := false
	for _, e := range all {
		if namesToDrop[e.base] {
			dropped = true
			continue
		}
		kept = append(kept, e.text)
	}
	if !dropped {
		return byteEdit{}, false
	}

	// Remove the whole statement (including trailing newline) if nothing left.
	if len(kept) == 0 {
		return byteEdit{
			start: startOfLine(source, importNode.StartByte()),
			end:   endOfLine(source, importNode.EndByte()),
		}, true
	}

	// Otherwise replace the statement with a clean single-line form.
	module := ""
	if moduleNode != nil {
		module = schemaPython.Content(moduleNode, source)
	}
	newLine := "from " + module + " import " + strings.Join(kept, ", ")
	return byteEdit{
		start:       importNode.StartByte(),
		end:         importNode.EndByte(),
		replacement: []byte(newLine),
	}, true
}

// importStatementImportsOnlyModule returns true if the given `import_statement`
// node is exactly `import <moduleName>` (single module, no alias, no comma list).
func importStatementImportsOnlyModule(node *sitter.Node, source []byte, moduleName string) bool {
	names := schemaPython.NamedChildren(node)
	if len(names) != 1 {
		return false
	}
	if names[0].Type() != "dotted_name" {
		return false
	}
	return schemaPython.Content(names[0], source) == moduleName
}

// addToCogImport ensures `localName` is available as a bare name by adding
// it to an existing `from cog import ...` line, or inserting a new import at
// the top of the file if none exists. If localName != "BaseModel", the name
// is inserted as an alias: `from cog import BaseModel as <localName>`.
func addToCogImport(ctx context.Context, source []byte, localName string) []byte {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return source
	}
	defer tree.Close()
	root := tree.RootNode()
	imports := schemaPython.CollectImports(root, source)

	// If the local name is already bound to cog.BaseModel, nothing to do.
	if entry, ok := imports.Names.Get(localName); ok &&
		entry.Module == "cog" && entry.Original == "BaseModel" {
		return source
	}

	// What we need to add to the cog import statement's name list:
	importFragment := "BaseModel"
	if localName != "BaseModel" {
		importFragment = "BaseModel as " + localName
	}

	// Try to append to an existing `from cog import ...` statement.
	for _, child := range schemaPython.NamedChildren(root) {
		if child.Type() != "import_from_statement" {
			continue
		}
		moduleNode := child.ChildByFieldName("module_name")
		if moduleNode == nil || schemaPython.Content(moduleNode, source) != "cog" {
			continue
		}
		edit, ok := editAppendRawImport(child, source, localName, importFragment)
		if !ok {
			continue
		}
		return applyEdits(source, []byteEdit{edit})
	}

	// No `from cog import` found — insert a new line at the top of the file,
	// but after any leading `from __future__ ...` imports or module docstring.
	insertAt := moduleInsertionPoint(root, source)
	newLine := []byte("from cog import " + importFragment + "\n")
	result := make([]byte, 0, len(source)+len(newLine))
	result = append(result, source[:insertAt]...)
	result = append(result, newLine...)
	result = append(result, source[insertAt:]...)
	return result
}

// editAppendRawImport returns an edit that appends the raw import fragment
// (e.g. "BaseModel" or "BaseModel as CogBaseModel") to an existing
// import_from_statement's name list. `localName` is the local identifier
// that will be bound; if a name with that local binding already exists,
// this is a no-op.
func editAppendRawImport(importNode *sitter.Node, source []byte, localName, importFragment string) (byteEdit, bool) {
	moduleNode := importNode.ChildByFieldName("module_name")
	if moduleNode == nil {
		return byteEdit{}, false
	}
	module := schemaPython.Content(moduleNode, source)

	// Collect existing names, preserving any aliases. If the name is already
	// bound locally, do nothing.
	type entry struct {
		text  string
		local string
	}
	var all []entry
	for _, child := range schemaPython.AllChildren(importNode) {
		switch child.Type() {
		case "dotted_name":
			if child.StartByte() == moduleNode.StartByte() {
				continue
			}
			n := schemaPython.Content(child, source)
			all = append(all, entry{text: n, local: n})
		case "aliased_import":
			orig := child.ChildByFieldName("name")
			alias := child.ChildByFieldName("alias")
			if orig == nil {
				continue
			}
			local := schemaPython.Content(orig, source)
			if alias != nil {
				local = schemaPython.Content(alias, source)
			}
			all = append(all, entry{
				text:  schemaPython.Content(child, source),
				local: local,
			})
		case "import_list":
			for _, ic := range schemaPython.AllChildren(child) {
				switch ic.Type() {
				case "dotted_name":
					n := schemaPython.Content(ic, source)
					all = append(all, entry{text: n, local: n})
				case "aliased_import":
					orig := ic.ChildByFieldName("name")
					alias := ic.ChildByFieldName("alias")
					if orig == nil {
						continue
					}
					local := schemaPython.Content(orig, source)
					if alias != nil {
						local = schemaPython.Content(alias, source)
					}
					all = append(all, entry{
						text:  schemaPython.Content(ic, source),
						local: local,
					})
				}
			}
		}
	}

	for _, e := range all {
		if e.local == localName {
			return byteEdit{}, false
		}
	}

	names := make([]string, 0, len(all)+1)
	for _, e := range all {
		names = append(names, e.text)
	}
	names = append(names, importFragment)

	newLine := "from " + module + " import " + strings.Join(names, ", ")
	return byteEdit{
		start:       importNode.StartByte(),
		end:         importNode.EndByte(),
		replacement: []byte(newLine),
	}, true
}

// moduleInsertionPoint returns the byte offset at which a new import can be
// inserted — after any leading module docstring and `from __future__` imports.
func moduleInsertionPoint(root *sitter.Node, source []byte) uint32 {
	var offset uint32
	for _, child := range schemaPython.NamedChildren(root) {
		switch child.Type() {
		case "expression_statement":
			// A leading string literal is a module docstring; skip it.
			if child.NamedChildCount() == 1 && child.NamedChild(0).Type() == "string" {
				offset = endOfLine(source, child.EndByte())
				continue
			}
			return offset
		case "import_from_statement":
			moduleNode := child.ChildByFieldName("module_name")
			if moduleNode != nil && schemaPython.Content(moduleNode, source) == "__future__" {
				offset = endOfLine(source, child.EndByte())
				continue
			}
			return offset
		default:
			return offset
		}
	}
	return offset
}

// startOfLine returns the byte offset of the start of the line containing `pos`.
func startOfLine(source []byte, pos uint32) uint32 {
	for pos > 0 && source[pos-1] != '\n' {
		pos--
	}
	return pos
}

// endOfLine returns the byte offset just past the newline that terminates the
// line containing `pos` (or len(source) if there is no trailing newline).
func endOfLine(source []byte, pos uint32) uint32 {
	for int(pos) < len(source) && source[pos] != '\n' {
		pos++
	}
	if int(pos) < len(source) {
		pos++
	}
	return pos
}

// applyEdits applies a list of byte-range edits to `source` and returns the
// resulting buffer. Edits must not overlap. They are sorted descending by
// start so earlier byte offsets remain valid during application.
func applyEdits(source []byte, edits []byteEdit) []byte {
	if len(edits) == 0 {
		return source
	}
	sorted := make([]byteEdit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].start > sorted[j].start
	})

	result := make([]byte, len(source))
	copy(result, source)
	for _, e := range sorted {
		if int(e.start) > len(result) {
			continue
		}
		end := min(int(e.end), len(result))
		// Replace result[e.start:end] with e.replacement.
		tail := append([]byte{}, result[end:]...)
		result = append(result[:e.start], e.replacement...)
		result = append(result, tail...)
	}
	return result
}
