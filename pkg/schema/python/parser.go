// Package python implements a tree-sitter based Python parser for extracting
// Cog predictor signatures. It walks the concrete syntax tree to extract
// imports, class definitions, function parameters with type annotations and
// default values, and Input() call keyword arguments.
//
// This parser is Python-specific. Future languages (e.g. Node.js) would get
// their own parser package under pkg/schema/.
package python

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/replicate/cog/pkg/schema"
)

// ParsePredictor parses Python source and extracts predictor information.
// predictRef is the class or function name (e.g. "Predictor" or "predict").
// mode controls whether we look for predict or train method.
// sourceDir is the project root for resolving cross-file imports. Pass "" if unknown.
func ParsePredictor(source []byte, predictRef string, mode schema.Mode, sourceDir string) (*schema.PredictorInfo, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, schema.WrapError(schema.ErrParse, "tree-sitter parse failed", err)
	}

	root := tree.RootNode()

	// 1. Collect imports
	imports := collectImports(root, source)

	// 2. Collect module-level variable assignments
	moduleScope := collectModuleScope(root, source)

	// 3. Collect BaseModel subclasses (local file first, then cross-file)
	modelClasses := collectModelClasses(root, source, imports)
	if sourceDir != "" {
		resolveExternalModels(sourceDir, imports, modelClasses)
	}

	// 4. Collect Input() references from class attributes and static methods
	inputRegistry := collectInputRegistry(root, source, imports, moduleScope)

	// 5. Find the target predict/train function
	methodName := "predict"
	if mode == schema.ModeTrain {
		methodName = "train"
	}

	funcNode, err := findTargetFunction(root, source, predictRef, methodName)
	if err != nil {
		return nil, err
	}

	// 6. Check if method (has self first param)
	paramsNode := funcNode.ChildByFieldName("parameters")
	if paramsNode == nil {
		return nil, schema.WrapError(schema.ErrParse, "function has no parameters node", nil)
	}
	isMethod := firstParamIsSelf(paramsNode, source)

	// 7. Extract parameters
	inputs, err := extractInputs(paramsNode, source, methodName, isMethod, imports, inputRegistry, moduleScope)
	if err != nil {
		return nil, err
	}

	// 8. Extract return type
	returnAnn := funcNode.ChildByFieldName("return_type")
	if returnAnn == nil {
		return nil, schema.WrapError(schema.ErrMissingReturnType, methodName, nil)
	}
	returnTypeAnn, err := parseTypeAnnotation(returnAnn, source)
	if err != nil {
		return nil, err
	}
	output, err := schema.ResolveSchemaType(returnTypeAnn, imports, modelClasses)
	if err != nil {
		return nil, err
	}

	return &schema.PredictorInfo{
		Inputs: inputs,
		Output: output,
		Mode:   mode,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// namedChildren returns all named children of a node.
func namedChildren(n *sitter.Node) []*sitter.Node {
	count := int(n.NamedChildCount())
	result := make([]*sitter.Node, 0, count)
	for i := range count {
		result = append(result, n.NamedChild(i))
	}
	return result
}

// allChildren returns all children (named and anonymous) of a node.
func allChildren(n *sitter.Node) []*sitter.Node {
	count := int(n.ChildCount())
	result := make([]*sitter.Node, 0, count)
	for i := range count {
		result = append(result, n.Child(i))
	}
	return result
}

// content returns the source text for a node.
func content(n *sitter.Node, source []byte) string {
	return n.Content(source)
}

// ---------------------------------------------------------------------------
// Import collection
// ---------------------------------------------------------------------------

func collectImports(root *sitter.Node, source []byte) *schema.ImportContext {
	ctx := schema.NewImportContext()

	for _, child := range namedChildren(root) {
		if child.Type() == "import_from_statement" {
			parseImportFrom(child, source, ctx)
		}
	}

	// Always include Python builtins
	for _, builtin := range []string{"str", "int", "float", "bool", "list", "dict", "set"} {
		if _, ok := ctx.Names.Get(builtin); !ok {
			ctx.Names.Set(builtin, schema.ImportEntry{Module: "builtins", Original: builtin})
		}
	}
	if _, ok := ctx.Names.Get("None"); !ok {
		ctx.Names.Set("None", schema.ImportEntry{Module: "builtins", Original: "None"})
	}

	return ctx
}

func parseImportFrom(node *sitter.Node, source []byte, ctx *schema.ImportContext) {
	moduleNode := node.ChildByFieldName("module_name")
	if moduleNode == nil {
		return
	}
	module := content(moduleNode, source)

	for _, child := range allChildren(node) {
		switch child.Type() {
		case "dotted_name":
			// Single import: `from X import name`
			// Skip if this is the module_name itself
			if child.StartByte() != moduleNode.StartByte() {
				name := content(child, source)
				ctx.Names.Set(name, schema.ImportEntry{Module: module, Original: name})
			}
		case "aliased_import":
			// Single aliased import: `from X import name as alias`
			origNode := child.ChildByFieldName("name")
			aliasNode := child.ChildByFieldName("alias")
			orig := ""
			if origNode != nil {
				orig = content(origNode, source)
			}
			alias := orig
			if aliasNode != nil {
				alias = content(aliasNode, source)
			}
			ctx.Names.Set(alias, schema.ImportEntry{Module: module, Original: orig})
		case "import_list":
			for _, importChild := range allChildren(child) {
				switch importChild.Type() {
				case "dotted_name":
					name := content(importChild, source)
					ctx.Names.Set(name, schema.ImportEntry{Module: module, Original: name})
				case "aliased_import":
					origNode := importChild.ChildByFieldName("name")
					aliasNode := importChild.ChildByFieldName("alias")
					orig := ""
					if origNode != nil {
						orig = content(origNode, source)
					}
					alias := orig
					if aliasNode != nil {
						alias = content(aliasNode, source)
					}
					ctx.Names.Set(alias, schema.ImportEntry{Module: module, Original: orig})
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Module scope collection
// ---------------------------------------------------------------------------

type moduleScope map[string]schema.DefaultValue

func collectModuleScope(root *sitter.Node, source []byte) moduleScope {
	scope := make(moduleScope)
	for _, child := range namedChildren(root) {
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
		name := content(left, source)

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

// resolveDefaultExpr tries to resolve an expression to a DefaultValue by
// literal parsing, then falling back to module scope lookup for identifiers.
func resolveDefaultExpr(node *sitter.Node, source []byte, scope moduleScope) (schema.DefaultValue, bool) {
	if val, ok := parseDefaultValue(node, source); ok {
		return val, true
	}
	if node.Type() == "identifier" {
		name := content(node, source)
		if val, ok := scope[name]; ok {
			return val, true
		}
	}
	return schema.DefaultValue{}, false
}

// resolveChoicesExpr tries to statically resolve a choices= expression.
func resolveChoicesExpr(node *sitter.Node, source []byte, scope moduleScope) ([]schema.DefaultValue, bool) {
	switch node.Type() {
	case "list":
		return parseListLiteral(node, source)

	case "identifier":
		name := content(node, source)
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
		// Only handle + (list concatenation)
		hasPlus := false
		for _, c := range allChildren(node) {
			if !c.IsNamed() && content(c, source) == "+" {
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

// resolveChoicesCall resolves list(X.keys()) or list(X.values()).
func resolveChoicesCall(node *sitter.Node, source []byte, scope moduleScope) ([]schema.DefaultValue, bool) {
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil || content(funcNode, source) != "list" {
		return nil, false
	}

	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil, false
	}

	// Find the single positional argument
	var arg *sitter.Node
	for _, c := range namedChildren(args) {
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

	varName := content(obj, source)
	methodName := content(attr, source)

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

// ---------------------------------------------------------------------------
// BaseModel subclass collection
// ---------------------------------------------------------------------------

func collectModelClasses(root *sitter.Node, source []byte, imports *schema.ImportContext) schema.ModelClassMap {
	models := schema.NewOrderedMap[string, []schema.ModelField]()

	for _, child := range namedChildren(root) {
		classNode := unwrapClass(child)
		if classNode == nil {
			continue
		}

		nameNode := classNode.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		className := content(nameNode, source)

		if !inheritsFromBaseModel(classNode, source, imports) {
			continue
		}

		fields := extractClassAnnotations(classNode, source)
		models.Set(className, fields)
	}
	return models
}

// resolveExternalModels looks at imports that brought in names not yet in
// modelClasses, attempts to find the corresponding .py file on disk, parses
// it, and merges any BaseModel subclasses into modelClasses.
//
// This handles every local import permutation:
//
//	from .types import Output          → <sourceDir>/types.py
//	from types import Output           → <sourceDir>/types.py
//	from models.output import Result   → <sourceDir>/models/output.py
//	from .models.output import Result  → <sourceDir>/models/output.py
//	from my_app.types import Foo       → <sourceDir>/my_app/types.py
//
// Non-local imports (stdlib, pip packages) are skipped because the file
// won't exist on disk.
func resolveExternalModels(sourceDir string, imports *schema.ImportContext, models schema.ModelClassMap) {
	// Track which modules we've already tried so we don't re-parse.
	tried := make(map[string]bool)

	imports.Names.Entries(func(localName string, entry schema.ImportEntry) {
		// Already resolved locally — skip.
		if _, ok := models.Get(localName); ok {
			return
		}

		module := entry.Module
		if !tried[module] {
			tried[module] = true

			// Skip known non-local modules.
			if isKnownExternalModule(module) {
				return
			}

			// Convert module path to filesystem path and try to find it.
			pyPath := moduleToFilePath(module)
			if pyPath == "" {
				return
			}

			fullPath := filepath.Join(sourceDir, pyPath)
			source, err := os.ReadFile(fullPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// File doesn't exist — it's an external package, not local.
					return
				}
				fmt.Fprintf(os.Stderr, "cog: warning: failed to read %q: %v\n", fullPath, err)
				return
			}

			// Parse the file and extract BaseModel subclasses.
			parser := sitter.NewParser()
			parser.SetLanguage(python.GetLanguage())
			tree, err := parser.ParseCtx(context.Background(), nil, source)
			if err != nil {
				fmt.Fprintf(os.Stderr, "cog: warning: failed to parse %q: %v\n", fullPath, err)
				return
			}

			fileImports := collectImports(tree.RootNode(), source)
			fileModels := collectModelClasses(tree.RootNode(), source, fileImports)

			// Merge discovered models into the caller's map.
			fileModels.Entries(func(name string, fields []schema.ModelField) {
				if _, exists := models.Get(name); !exists {
					models.Set(name, fields)
				}
			})
		}

		// Handle aliases: "from X import MyOutput as Output"
		// localName is "Output", entry.Original is "MyOutput".
		// If we resolved "MyOutput" from the file, also register it under "Output".
		if localName != entry.Original {
			if fields, ok := models.Get(entry.Original); ok {
				if _, exists := models.Get(localName); !exists {
					models.Set(localName, fields)
				}
			}
		}
	})
}

// moduleToFilePath converts a Python module path to a relative .py file path.
//
//	".types"          → "types.py"
//	"types"           → "types.py"
//	".models.output"  → "models/output.py"
//	"models.output"   → "models/output.py"
//	"cog"             → "cog.py"  (will fail os.ReadFile → skipped)
func moduleToFilePath(module string) string {
	// Strip leading dots (relative import markers).
	clean := strings.TrimLeft(module, ".")
	if clean == "" {
		return ""
	}
	// Replace dots with path separators.
	parts := strings.Split(clean, ".")
	return filepath.Join(parts...) + ".py"
}

// isKnownExternalModule returns true for modules that are definitely not
// local project files — stdlib, well-known packages, etc.
func isKnownExternalModule(module string) bool {
	// Extract the top-level package name.
	top := module
	if i := strings.Index(module, "."); i > 0 {
		top = module[:i]
	}
	top = strings.TrimLeft(top, ".")

	switch top {
	case "builtins", "typing", "typing_extensions",
		"collections", "abc", "enum", "dataclasses",
		"os", "sys", "io", "json", "re", "math", "pathlib",
		"functools", "itertools", "contextlib",
		"concurrent", "asyncio", "multiprocessing", "threading",
		"logging", "warnings", "unittest", "pytest",
		"numpy", "torch", "tensorflow", "jax", "scipy", "sklearn",
		"transformers", "diffusers", "accelerate", "safetensors",
		"PIL", "cv2", "skimage",
		"requests", "httpx", "aiohttp", "fastapi", "flask",
		"pydantic", "cog":
		return true
	}
	return false
}

func unwrapClass(node *sitter.Node) *sitter.Node {
	if node.Type() == "class_definition" {
		return node
	}
	if node.Type() == "decorated_definition" {
		for _, c := range namedChildren(node) {
			if c.Type() == "class_definition" {
				return c
			}
		}
	}
	return nil
}

func unwrapFunction(node *sitter.Node) *sitter.Node {
	if node.Type() == "function_definition" {
		return node
	}
	if node.Type() == "decorated_definition" {
		for _, c := range namedChildren(node) {
			if c.Type() == "function_definition" {
				return c
			}
		}
	}
	return nil
}

func inheritsFromBaseModel(classNode *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	supers := classNode.ChildByFieldName("superclasses")
	if supers == nil {
		return false
	}
	for _, child := range allChildren(supers) {
		switch child.Type() {
		case "identifier":
			name := content(child, source)
			if imports.IsBaseModel(name) || name == "BaseModel" {
				return true
			}
		case "attribute":
			// Handle dotted access: pydantic.BaseModel, cog.BaseModel
			text := content(child, source)
			if strings.HasSuffix(text, ".BaseModel") {
				return true
			}
		}
	}
	return false
}

func extractClassAnnotations(classNode *sitter.Node, source []byte) []schema.ModelField {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	var fields []schema.ModelField
	for _, child := range namedChildren(body) {
		node := child
		if child.Type() == "expression_statement" && child.NamedChildCount() == 1 {
			node = child.NamedChild(0)
		}

		switch node.Type() {
		case "assignment":
			if f, ok := parseAnnotatedAssignment(node, source); ok {
				fields = append(fields, f)
			}
		case "type":
			if f, ok := parseBareAnnotation(node, source); ok {
				fields = append(fields, f)
			}
		}
	}
	return fields
}

func parseAnnotatedAssignment(node *sitter.Node, source []byte) (schema.ModelField, bool) {
	left := node.ChildByFieldName("left")
	typeNode := node.ChildByFieldName("type")
	if left == nil || typeNode == nil || left.Type() != "identifier" {
		return schema.ModelField{}, false
	}

	name := content(left, source)
	typeAnn, err := parseTypeAnnotation(typeNode, source)
	if err != nil {
		return schema.ModelField{}, false
	}

	var def *schema.DefaultValue
	if right := node.ChildByFieldName("right"); right != nil {
		if v, ok := parseDefaultValue(right, source); ok {
			def = &v
		}
	}

	return schema.ModelField{Name: name, Type: typeAnn, Default: def}, true
}

func parseBareAnnotation(node *sitter.Node, source []byte) (schema.ModelField, bool) {
	text := strings.TrimSpace(content(node, source))
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return schema.ModelField{}, false
	}
	name := strings.TrimSpace(parts[0])
	typeStr := strings.TrimSpace(parts[1])

	if name == "" || (name[0] != '_' && (name[0] < 'a' || name[0] > 'z') && (name[0] < 'A' || name[0] > 'Z')) {
		return schema.ModelField{}, false
	}

	typeAnn, ok := parseTypeFromString(typeStr)
	if !ok {
		return schema.ModelField{}, false
	}

	return schema.ModelField{Name: name, Type: typeAnn, Default: nil}, true
}

func parseTypeFromString(s string) (schema.TypeAnnotation, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return schema.TypeAnnotation{}, false
	}

	// Union: X | Y
	if strings.Contains(s, "|") {
		parts := strings.Split(s, "|")
		var members []schema.TypeAnnotation
		for _, p := range parts {
			m, ok := parseTypeFromString(strings.TrimSpace(p))
			if !ok {
				return schema.TypeAnnotation{}, false
			}
			members = append(members, m)
		}
		if len(members) >= 2 {
			return schema.TypeAnnotation{Kind: schema.TypeAnnotUnion, Args: members}, true
		}
		return schema.TypeAnnotation{}, false
	}

	// Generic: X[Y] or X[Y, Z]
	bracketPos := strings.Index(s, "[")
	if bracketPos >= 0 && strings.HasSuffix(s, "]") {
		outer := strings.TrimSpace(s[:bracketPos])
		innerStr := s[bracketPos+1 : len(s)-1]

		// Split on top-level commas (handles Union[str, None], etc.)
		parts := splitTopLevelCommas(innerStr)
		var args []schema.TypeAnnotation
		for _, p := range parts {
			arg, ok := parseTypeFromString(strings.TrimSpace(p))
			if !ok {
				return schema.TypeAnnotation{}, false
			}
			args = append(args, arg)
		}
		if len(args) == 0 {
			return schema.TypeAnnotation{}, false
		}
		return schema.TypeAnnotation{Kind: schema.TypeAnnotGeneric, Name: outer, Args: args}, true
	}

	// Simple identifier
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return schema.TypeAnnotation{}, false
		}
	}
	return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: s}, true
}

// splitTopLevelCommas splits a string on commas that are not nested inside brackets.
// e.g. "str, None" → ["str", "None"], "List[str], None" → ["List[str]", "None"]
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// ---------------------------------------------------------------------------
// InputRegistry — resolves ClassName.attr and ClassName.method(args)
// ---------------------------------------------------------------------------

type inputCallInfo struct {
	Default     *schema.DefaultValue
	Description *string
	GE          *float64
	LE          *float64
	MinLength   *uint64
	MaxLength   *uint64
	Regex       *string
	Choices     []schema.DefaultValue
	Deprecated  *bool
}

type inputMethodInfo struct {
	ParamNames []string
	BaseInfo   inputCallInfo
}

type inputRegistry struct {
	Attributes map[string]inputCallInfo
	Methods    map[string]inputMethodInfo
}

func newInputRegistry() *inputRegistry {
	return &inputRegistry{
		Attributes: make(map[string]inputCallInfo),
		Methods:    make(map[string]inputMethodInfo),
	}
}

func collectInputRegistry(root *sitter.Node, source []byte, imports *schema.ImportContext, scope moduleScope) *inputRegistry {
	registry := newInputRegistry()

	for _, child := range namedChildren(root) {
		classNode := unwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		className := content(nameNode, source)

		body := classNode.ChildByFieldName("body")
		if body == nil {
			continue
		}

		for _, stmt := range namedChildren(body) {
			inner := stmt
			if stmt.Type() == "expression_statement" && stmt.NamedChildCount() == 1 {
				inner = stmt.NamedChild(0)
			}

			if inner.Type() == "assignment" {
				collectInputAttribute(className, inner, source, imports, scope, registry)
			}

			if funcNode := unwrapFunction(inner); funcNode != nil {
				collectInputMethod(className, funcNode, source, imports, scope, registry)
			}
		}
	}

	return registry
}

func collectInputAttribute(className string, assignment *sitter.Node, source []byte, imports *schema.ImportContext, scope moduleScope, registry *inputRegistry) {
	left := assignment.ChildByFieldName("left")
	if left == nil || left.Type() != "identifier" {
		return
	}
	attrName := content(left, source)

	right := assignment.ChildByFieldName("right")
	if right == nil || !isInputCall(right, source, imports) {
		return
	}

	key := className + "." + attrName
	info, err := parseInputCall(right, source, key, scope)
	if err != nil {
		return
	}
	registry.Attributes[key] = info
}

func collectInputMethod(className string, funcNode *sitter.Node, source []byte, imports *schema.ImportContext, scope moduleScope, registry *inputRegistry) {
	nameNode := funcNode.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := content(nameNode, source)

	params := funcNode.ChildByFieldName("parameters")
	if params == nil {
		return
	}

	var paramNames []string
	for _, param := range allChildren(params) {
		switch param.Type() {
		case "identifier":
			name := content(param, source)
			if name != "self" && name != "cls" {
				paramNames = append(paramNames, name)
			}
		case "typed_parameter":
			// typed_parameter has no "name" field; first identifier child is the name
			for j := 0; j < int(param.NamedChildCount()); j++ {
				c := param.NamedChild(j)
				if c.Type() == "identifier" {
					name := content(c, source)
					if name != "self" && name != "cls" {
						paramNames = append(paramNames, name)
					}
					break
				}
			}
		case "typed_default_parameter", "default_parameter":
			if n := param.ChildByFieldName("name"); n != nil {
				name := content(n, source)
				if name != "self" && name != "cls" {
					paramNames = append(paramNames, name)
				}
			}
		}
	}

	body := funcNode.ChildByFieldName("body")
	if body == nil {
		return
	}

	inputCall := findReturnInputCall(body, source, imports)
	if inputCall == nil {
		return
	}

	key := className + "." + methodName
	info, err := parseInputCall(inputCall, source, key, scope)
	if err != nil {
		return
	}
	registry.Methods[key] = inputMethodInfo{ParamNames: paramNames, BaseInfo: info}
}

func findReturnInputCall(body *sitter.Node, source []byte, imports *schema.ImportContext) *sitter.Node {
	for _, child := range namedChildren(body) {
		if child.Type() == "return_statement" {
			if child.NamedChildCount() > 0 {
				expr := child.NamedChild(0)
				if isInputCall(expr, source, imports) {
					return expr
				}
			}
		}
	}
	return nil
}

func resolveInputReference(node *sitter.Node, source []byte, registry *inputRegistry) (inputCallInfo, bool) {
	switch node.Type() {
	case "attribute":
		text := content(node, source)
		info, ok := registry.Attributes[text]
		return info, ok

	case "call":
		funcNode := node.ChildByFieldName("function")
		if funcNode == nil || funcNode.Type() != "attribute" {
			return inputCallInfo{}, false
		}
		key := content(funcNode, source)
		methodInfo, ok := registry.Methods[key]
		if !ok {
			return inputCallInfo{}, false
		}

		resolved := methodInfo.BaseInfo

		args := node.ChildByFieldName("arguments")
		if args == nil {
			return resolved, true
		}

		// Build param_name -> call-site value map
		argValues := make(map[string]*sitter.Node)
		positionalIdx := 0
		for _, arg := range namedChildren(args) {
			if arg.Type() == "keyword_argument" {
				nameNode := arg.ChildByFieldName("name")
				valNode := arg.ChildByFieldName("value")
				if nameNode != nil && valNode != nil {
					argValues[content(nameNode, source)] = valNode
				}
			} else if positionalIdx < len(methodInfo.ParamNames) {
				argValues[methodInfo.ParamNames[positionalIdx]] = arg
				positionalIdx++
			}
		}

		// Override with call-site values
		for paramName, callNode := range argValues {
			switch paramName {
			case "default":
				if val, ok := parseDefaultValue(callNode, source); ok {
					resolved.Default = &val
				}
			case "description":
				if s, ok := parseStringLiteral(callNode, source); ok {
					resolved.Description = &s
				}
			case "ge":
				if n, ok := parseNumberLiteral(callNode, source); ok {
					resolved.GE = &n
				}
			case "le":
				if n, ok := parseNumberLiteral(callNode, source); ok {
					resolved.LE = &n
				}
			}
		}

		return resolved, true
	}
	return inputCallInfo{}, false
}

// ---------------------------------------------------------------------------
// Target function finding
// ---------------------------------------------------------------------------

func findTargetFunction(root *sitter.Node, source []byte, predictRef, methodName string) (*sitter.Node, error) {
	// First: look for a class with this name
	for _, child := range namedChildren(root) {
		classNode := unwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode != nil && content(nameNode, source) == predictRef {
			return findMethodInClass(classNode, source, predictRef, methodName)
		}
	}

	// Second: look for standalone function
	for _, child := range namedChildren(root) {
		funcNode := unwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode != nil {
			name := content(nameNode, source)
			if name == predictRef || name == methodName {
				return funcNode, nil
			}
		}
	}

	return nil, schema.WrapError(schema.ErrPredictorNotFound, predictRef, nil)
}

func findMethodInClass(classNode *sitter.Node, source []byte, className, methodName string) (*sitter.Node, error) {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil, schema.WrapError(schema.ErrParse, fmt.Sprintf("class %s has no body", className), nil)
	}

	for _, child := range namedChildren(body) {
		funcNode := unwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode != nil && content(nameNode, source) == methodName {
			return funcNode, nil
		}
	}

	return nil, schema.WrapError(schema.ErrMethodNotFound, fmt.Sprintf("%s.%s not found", className, methodName), nil)
}

// ---------------------------------------------------------------------------
// Parameter extraction
// ---------------------------------------------------------------------------

func firstParamIsSelf(params *sitter.Node, source []byte) bool {
	for _, child := range allChildren(params) {
		if child.Type() == "identifier" {
			return content(child, source) == "self"
		}
	}
	return false
}

func extractInputs(
	paramsNode *sitter.Node,
	source []byte,
	methodName string,
	skipSelf bool,
	imports *schema.ImportContext,
	registry *inputRegistry,
	scope moduleScope,
) (*schema.OrderedMap[string, schema.InputField], error) {
	inputs := schema.NewOrderedMap[string, schema.InputField]()
	order := 0
	seenSelf := false

	for _, child := range allChildren(paramsNode) {
		switch child.Type() {
		case "identifier":
			if !seenSelf && skipSelf {
				name := content(child, source)
				if name == "self" {
					seenSelf = true
					continue
				}
			}

		case "typed_parameter":
			input, err := parseTypedParameter(child, source, order, methodName, imports)
			if err != nil {
				return nil, err
			}
			inputs.Set(input.Name, input)
			order++

		case "typed_default_parameter":
			input, err := parseTypedDefaultParameter(child, source, order, methodName, imports, registry, scope)
			if err != nil {
				return nil, err
			}
			inputs.Set(input.Name, input)
			order++

		case "default_parameter":
			nameNode := child.ChildByFieldName("name")
			paramName := "<unknown>"
			if nameNode != nil {
				paramName = content(nameNode, source)
			}
			return nil, schema.WrapError(schema.ErrMissingTypeAnnotation, fmt.Sprintf("parameter '%s' on %s has no type annotation", paramName, methodName), nil)
		}
	}

	return inputs, nil
}

func parseTypedParameter(node *sitter.Node, source []byte, order int, methodName string, imports *schema.ImportContext) (schema.InputField, error) {
	// typed_parameter has no "name" field in the Python grammar.
	// Structure is: identifier ":" type
	var name string
	var typeNode *sitter.Node
	for i := 0; i < int(node.NamedChildCount()); i++ {
		c := node.NamedChild(i)
		switch c.Type() {
		case "identifier":
			if name == "" {
				name = content(c, source)
			}
		case "type":
			typeNode = c
		}
	}
	if name == "" {
		return schema.InputField{}, schema.WrapError(schema.ErrParse, "typed_parameter has no identifier", nil)
	}
	if typeNode == nil {
		return schema.InputField{}, schema.WrapError(schema.ErrMissingTypeAnnotation, fmt.Sprintf("parameter '%s' on %s has no type annotation", name, methodName), nil)
	}

	typeAnn, err := parseTypeAnnotation(typeNode, source)
	if err != nil {
		return schema.InputField{}, err
	}
	fieldType, err := schema.ResolveFieldType(typeAnn, imports)
	if err != nil {
		return schema.InputField{}, err
	}

	return schema.InputField{
		Name:      name,
		Order:     order,
		FieldType: fieldType,
	}, nil
}

func parseTypedDefaultParameter(
	node *sitter.Node,
	source []byte,
	order int,
	methodName string,
	imports *schema.ImportContext,
	registry *inputRegistry,
	scope moduleScope,
) (schema.InputField, error) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return schema.InputField{}, schema.WrapError(schema.ErrParse, "typed_default_parameter has no name", nil)
	}
	name := content(nameNode, source)

	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return schema.InputField{}, schema.WrapError(schema.ErrMissingTypeAnnotation, fmt.Sprintf("parameter '%s' on %s has no type annotation", name, methodName), nil)
	}

	typeAnn, err := parseTypeAnnotation(typeNode, source)
	if err != nil {
		return schema.InputField{}, err
	}
	fieldType, err := schema.ResolveFieldType(typeAnn, imports)
	if err != nil {
		return schema.InputField{}, err
	}

	valNode := node.ChildByFieldName("value")

	if valNode != nil {
		// 1. Direct Input() call
		if isInputCall(valNode, source, imports) {
			info, err := parseInputCall(valNode, source, name, scope)
			if err != nil {
				return schema.InputField{}, err
			}
			return schema.InputField{
				Name:        name,
				Order:       order,
				FieldType:   fieldType,
				Default:     info.Default,
				Description: info.Description,
				GE:          info.GE,
				LE:          info.LE,
				MinLength:   info.MinLength,
				MaxLength:   info.MaxLength,
				Regex:       info.Regex,
				Choices:     info.Choices,
				Deprecated:  info.Deprecated,
			}, nil
		}

		// 2. Reference to Input() via class attribute or static method
		if info, ok := resolveInputReference(valNode, source, registry); ok {
			return schema.InputField{
				Name:        name,
				Order:       order,
				FieldType:   fieldType,
				Default:     info.Default,
				Description: info.Description,
				GE:          info.GE,
				LE:          info.LE,
				MinLength:   info.MinLength,
				MaxLength:   info.MaxLength,
				Regex:       info.Regex,
				Choices:     info.Choices,
				Deprecated:  info.Deprecated,
			}, nil
		}

		// 3. Plain default — must be statically resolvable
		if def, ok := resolveDefaultExpr(valNode, source, scope); ok {
			return schema.InputField{
				Name:      name,
				Order:     order,
				FieldType: fieldType,
				Default:   &def,
			}, nil
		}

		// Can't resolve — hard error
		valText := content(valNode, source)
		return schema.InputField{}, schema.WrapError(schema.ErrDefaultNotResolvable,
			fmt.Sprintf("parameter '%s': default `%s` cannot be statically resolved", name, valText), nil)
	}

	// No default — required parameter
	return schema.InputField{
		Name:      name,
		Order:     order,
		FieldType: fieldType,
	}, nil
}

// ---------------------------------------------------------------------------
// Type annotation parsing
// ---------------------------------------------------------------------------

func parseTypeAnnotation(node *sitter.Node, source []byte) (schema.TypeAnnotation, error) {
	// Unwrap `type` wrapper node
	n := node
	if n.Type() == "type" && n.NamedChildCount() > 0 {
		n = n.NamedChild(0)
	}

	switch n.Type() {
	case "identifier":
		return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: content(n, source)}, nil

	case "subscript":
		value := n.ChildByFieldName("value")
		if value == nil {
			return schema.TypeAnnotation{}, schema.WrapError(schema.ErrParse, "subscript has no value", nil)
		}
		outer := content(value, source)

		var args []schema.TypeAnnotation
		for _, child := range namedChildren(n) {
			// Skip the outer identifier (the value field)
			if child.StartByte() == value.StartByte() {
				continue
			}
			arg, err := parseTypeAnnotation(child, source)
			if err != nil {
				return schema.TypeAnnotation{}, err
			}
			args = append(args, arg)
		}

		if len(args) == 0 {
			return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: outer}, nil
		}
		return schema.TypeAnnotation{Kind: schema.TypeAnnotGeneric, Name: outer, Args: args}, nil

	case "binary_operator":
		left := n.ChildByFieldName("left")
		right := n.ChildByFieldName("right")
		if left == nil || right == nil {
			return schema.TypeAnnotation{}, schema.WrapError(schema.ErrParse, "binary_operator missing operand", nil)
		}

		// Check that operator is |
		isUnion := false
		for _, c := range allChildren(n) {
			if !c.IsNamed() && content(c, source) == "|" {
				isUnion = true
				break
			}
		}
		if !isUnion {
			return schema.TypeAnnotation{}, errUnsupported("non-union binary operator in type annotation")
		}

		leftAnn, err := parseTypeAnnotation(left, source)
		if err != nil {
			return schema.TypeAnnotation{}, err
		}
		rightAnn, err := parseTypeAnnotation(right, source)
		if err != nil {
			return schema.TypeAnnotation{}, err
		}

		// Flatten nested unions
		var members []schema.TypeAnnotation
		if leftAnn.Kind == schema.TypeAnnotUnion {
			members = append(members, leftAnn.Args...)
		} else {
			members = append(members, leftAnn)
		}
		if rightAnn.Kind == schema.TypeAnnotUnion {
			members = append(members, rightAnn.Args...)
		} else {
			members = append(members, rightAnn)
		}

		return schema.TypeAnnotation{Kind: schema.TypeAnnotUnion, Args: members}, nil

	case "none":
		return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: "None"}, nil

	case "attribute":
		return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: content(n, source)}, nil

	case "string", "concatenated_string":
		text := content(n, source)
		inner := strings.TrimLeft(text, "\"'")
		inner = strings.TrimRight(inner, "\"'")
		if ann, ok := parseTypeFromString(inner); ok {
			return ann, nil
		}
		return schema.TypeAnnotation{}, errUnsupported(fmt.Sprintf("string annotation: %s", text))

	default:
		text := content(n, source)
		if ann, ok := parseTypeFromString(text); ok {
			return ann, nil
		}
		return schema.TypeAnnotation{}, errUnsupported(fmt.Sprintf("%s: %s", n.Type(), text))
	}
}

func errUnsupported(msg string) error {
	return &schema.SchemaError{Kind: schema.ErrUnsupportedType, Message: fmt.Sprintf("unsupported type: %s", msg)}
}

// ---------------------------------------------------------------------------
// Input() call parsing
// ---------------------------------------------------------------------------

func isInputCall(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	if node.Type() != "call" {
		return false
	}
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return false
	}
	name := content(funcNode, source)
	if name == "Input" {
		return true
	}
	if e, ok := imports.Names.Get(name); ok {
		return e.Module == "cog" && e.Original == "Input"
	}
	return false
}

func parseInputCall(node *sitter.Node, source []byte, paramName string, scope moduleScope) (inputCallInfo, error) {
	var info inputCallInfo

	args := node.ChildByFieldName("arguments")
	if args == nil {
		return info, nil
	}

	for _, child := range namedChildren(args) {
		if child.Type() != "keyword_argument" {
			continue
		}
		keyNode := child.ChildByFieldName("name")
		valNode := child.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}

		key := content(keyNode, source)
		switch key {
		case "default":
			val, ok := resolveDefaultExpr(valNode, source, scope)
			if !ok {
				none := schema.DefaultValue{Kind: schema.DefaultNone}
				val = none
			}
			info.Default = &val
		case "default_factory":
			return inputCallInfo{}, schema.WrapError(schema.ErrDefaultFactoryNotSupported, fmt.Sprintf("parameter '%s': default_factory is not supported in static schema generation", paramName), nil)
		case "description":
			if s, ok := parseStringLiteral(valNode, source); ok {
				info.Description = &s
			}
		case "ge":
			if n, ok := parseNumberLiteral(valNode, source); ok {
				info.GE = &n
			}
		case "le":
			if n, ok := parseNumberLiteral(valNode, source); ok {
				info.LE = &n
			}
		case "min_length":
			if n, ok := parseNumberLiteral(valNode, source); ok {
				u := uint64(n)
				info.MinLength = &u
			}
		case "max_length":
			if n, ok := parseNumberLiteral(valNode, source); ok {
				u := uint64(n)
				info.MaxLength = &u
			}
		case "regex":
			if s, ok := parseStringLiteral(valNode, source); ok {
				info.Regex = &s
			}
		case "choices":
			if items, ok := parseListLiteral(valNode, source); ok {
				info.Choices = items
			} else if items, ok := resolveChoicesExpr(valNode, source, scope); ok {
				info.Choices = items
			} else {
				return inputCallInfo{}, schema.WrapError(schema.ErrChoicesNotResolvable, fmt.Sprintf("parameter '%s': choices expression cannot be statically resolved", paramName), nil)
			}
		case "deprecated":
			if b, ok := parseBoolLiteral(valNode, source); ok {
				info.Deprecated = &b
			}
		}
	}

	return info, nil
}

// ---------------------------------------------------------------------------
// Literal parsing
// ---------------------------------------------------------------------------

func parseDefaultValue(node *sitter.Node, source []byte) (schema.DefaultValue, bool) {
	switch node.Type() {
	case "none":
		return schema.DefaultValue{Kind: schema.DefaultNone}, true
	case "true":
		return schema.DefaultValue{Kind: schema.DefaultBool, Bool: true}, true
	case "false":
		return schema.DefaultValue{Kind: schema.DefaultBool, Bool: false}, true
	case "integer":
		text := content(node, source)
		n, err := strconv.ParseInt(text, 0, 64)
		if err != nil {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultInt, Int: n}, true
	case "float":
		text := content(node, source)
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultFloat, Float: f}, true
	case "string", "concatenated_string":
		s, ok := parseStringLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultString, Str: s}, true
	case "list":
		items, ok := parseListLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultList, List: items}, true
	case "dictionary":
		keys, vals, ok := parseDictLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultDict, DictKeys: keys, DictVals: vals}, true
	case "set":
		items, ok := parseSetLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultSet, List: items}, true
	case "unary_operator":
		text := strings.TrimSpace(content(node, source))
		if n, err := strconv.ParseInt(text, 0, 64); err == nil {
			return schema.DefaultValue{Kind: schema.DefaultInt, Int: n}, true
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			return schema.DefaultValue{Kind: schema.DefaultFloat, Float: f}, true
		}
		return schema.DefaultValue{}, false
	case "tuple":
		var items []schema.DefaultValue
		for _, child := range namedChildren(node) {
			if val, ok := parseDefaultValue(child, source); ok {
				items = append(items, val)
			}
		}
		return schema.DefaultValue{Kind: schema.DefaultList, List: items}, true
	}
	return schema.DefaultValue{}, false
}

func parseStringLiteral(node *sitter.Node, source []byte) (string, bool) {
	text := content(node, source)
	if strings.HasPrefix(text, `"""`) || strings.HasPrefix(text, `'''`) {
		if len(text) >= 6 {
			return text[3 : len(text)-3], true
		}
		return "", false
	}
	if strings.HasPrefix(text, `"`) || strings.HasPrefix(text, `'`) {
		if len(text) >= 2 {
			return text[1 : len(text)-1], true
		}
		return "", false
	}
	if strings.HasPrefix(text, `r"`) || strings.HasPrefix(text, `r'`) {
		if len(text) >= 3 {
			return text[2 : len(text)-1], true
		}
		return "", false
	}
	return "", false
}

func parseNumberLiteral(node *sitter.Node, source []byte) (float64, bool) {
	text := strings.TrimSpace(content(node, source))
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func parseBoolLiteral(node *sitter.Node, source []byte) (bool, bool) {
	switch node.Type() {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	text := content(node, source)
	switch text {
	case "True":
		return true, true
	case "False":
		return false, true
	}
	return false, false
}

func parseListLiteral(node *sitter.Node, source []byte) ([]schema.DefaultValue, bool) {
	if node.Type() != "list" {
		return nil, false
	}
	var items []schema.DefaultValue
	for _, child := range namedChildren(node) {
		val, ok := parseDefaultValue(child, source)
		if !ok {
			return nil, false
		}
		items = append(items, val)
	}
	return items, true
}

func parseDictLiteral(node *sitter.Node, source []byte) ([]schema.DefaultValue, []schema.DefaultValue, bool) {
	if node.Type() != "dictionary" {
		return nil, nil, false
	}
	var keys, vals []schema.DefaultValue
	for _, child := range namedChildren(node) {
		if child.Type() == "pair" {
			keyNode := child.ChildByFieldName("key")
			valNode := child.ChildByFieldName("value")
			if keyNode == nil || valNode == nil {
				continue
			}
			k, ok1 := parseDefaultValue(keyNode, source)
			v, ok2 := parseDefaultValue(valNode, source)
			if ok1 && ok2 {
				keys = append(keys, k)
				vals = append(vals, v)
			}
		}
	}
	return keys, vals, true
}

func parseSetLiteral(node *sitter.Node, source []byte) ([]schema.DefaultValue, bool) {
	if node.Type() != "set" {
		return nil, false
	}
	var items []schema.DefaultValue
	for _, child := range namedChildren(node) {
		val, ok := parseDefaultValue(child, source)
		if !ok {
			return nil, false
		}
		items = append(items, val)
	}
	return items, true
}
