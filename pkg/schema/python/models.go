package python

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/replicate/cog/pkg/schema"
)

type modelParseContext struct {
	imports        *schema.ImportContext
	typedDicts     map[string]bool
	loadedModules  map[string]ModuleSummary
	sourcePath     string
	resolvedModels schema.ModelClassMap
}

func collectModelClasses(root *sitter.Node, source []byte, ctx *modelParseContext) schema.ModelClassMap {
	models := schema.NewOrderedMap[string, []schema.ModelField]()

	for _, child := range NamedChildren(root) {
		classNode := UnwrapClass(child)
		if classNode == nil {
			continue
		}

		nameNode := classNode.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		className := Content(nameNode, source)

		isBaseModel := InheritsFromBaseModel(classNode, source, ctx.imports)
		isTypedDict, requiredByDefault, parents := TypedDictClassInfo(classNode, source, ctx.imports, ctx.typedDicts)
		if !isBaseModel && !isTypedDict {
			continue
		}

		fields := extractClassAnnotations(classNode, source, ctx.imports, isTypedDict, requiredByDefault)
		if isTypedDict {
			fields = mergeModelFields(parentModelFields(models, ctx, parents), fields)
		}
		models.Set(className, fields)
		if isTypedDict {
			ctx.typedDicts[className] = true
		}
	}
	return models
}

func parentModelFields(models schema.ModelClassMap, ctx *modelParseContext, parents []string) [][]schema.ModelField {
	merged := make([][]schema.ModelField, 0, len(parents))
	for _, parent := range parents {
		if fields, ok := models.Get(parent); ok {
			merged = append(merged, fields)
			continue
		}
		if ctx.resolvedModels != nil {
			if fields, ok := ctx.resolvedModels.Get(parent); ok {
				merged = append(merged, fields)
			}
		}
	}
	return merged
}

func mergeModelFields(parents [][]schema.ModelField, own []schema.ModelField) []schema.ModelField {
	merged := make([]schema.ModelField, 0)
	index := make(map[string]int)
	appendField := func(field schema.ModelField) {
		if i, ok := index[field.Name]; ok {
			merged[i] = field
			return
		}
		index[field.Name] = len(merged)
		merged = append(merged, field)
	}
	for _, parentFields := range parents {
		for _, field := range parentFields {
			appendField(field)
		}
	}
	for _, field := range own {
		appendField(field)
	}
	return merged
}

func mergeDiscoveredModels(dst, src schema.ModelClassMap) {
	if src == nil {
		return
	}
	src.Entries(func(name string, fields []schema.ModelField) {
		if _, exists := dst.Get(name); !exists {
			dst.Set(name, fields)
		}
	})
}

func setDiscoveredModels(dst, src schema.ModelClassMap) {
	if src == nil {
		return
	}
	src.Entries(func(name string, fields []schema.ModelField) {
		dst.Set(name, fields)
	})
}

func setQualifiedModelAliases(dst schema.ModelClassMap, qualifier string, src schema.ModelClassMap) {
	if qualifier == "" || src == nil {
		return
	}
	src.Entries(func(name string, fields []schema.ModelField) {
		dst.Set(qualifier+"."+name, fields)
	})
}

func setQualifiedTypedDictAliases(dst map[string]bool, qualifier string, src map[string]bool) {
	if qualifier == "" {
		return
	}
	for name := range src {
		dst[qualifier+"."+name] = true
	}
}

func (ctx *modelParseContext) loadModelsFromModule(sourceDir, module string) schema.ModelClassMap {
	if ctx.loadedModules == nil {
		ctx.loadedModules = make(map[string]ModuleSummary)
	}

	if isKnownExternalModule(module) {
		return nil
	}

	pyPath := moduleToFilePath(module, ctx.sourcePath)
	if pyPath == "" {
		return nil
	}
	cacheKey := filepath.Clean(pyPath)
	if summary, ok := ctx.loadedModules[cacheKey]; ok {
		return summary.Models
	}
	ctx.loadedModules[cacheKey] = ModuleSummary{TypedDicts: map[string]bool{}, Models: schema.NewOrderedMap[string, []schema.ModelField](), SourcePath: cacheKey}

	fullPath := filepath.Join(sourceDir, pyPath)
	source, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			delete(ctx.loadedModules, cacheKey)
			return nil
		}
		fmt.Fprintf(os.Stderr, "cog: warning: failed to read %q: %v\n", fullPath, err)
		ctx.loadedModules[cacheKey] = ModuleSummary{TypedDicts: map[string]bool{}, Models: schema.NewOrderedMap[string, []schema.ModelField](), SourcePath: cacheKey}
		return nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cog: warning: failed to parse %q: %v\n", fullPath, err)
		ctx.loadedModules[cacheKey] = ModuleSummary{TypedDicts: map[string]bool{}, Models: schema.NewOrderedMap[string, []schema.ModelField](), SourcePath: cacheKey}
		return nil
	}

	fileCtx := &modelParseContext{imports: CollectImports(tree.RootNode(), source), typedDicts: make(map[string]bool), loadedModules: ctx.loadedModules, sourcePath: cacheKey}
	fileModels := collectModelClasses(tree.RootNode(), source, fileCtx)
	fileCtx.resolvedModels = fileModels
	resolveExternalModels(sourceDir, fileModels, fileCtx)
	fileCtx.resolvedModels = fileModels
	setDiscoveredModels(fileModels, collectModelClasses(tree.RootNode(), source, fileCtx))
	ctx.loadedModules[cacheKey] = ModuleSummary{Imports: fileCtx.imports, Models: fileModels, TypedDicts: fileCtx.typedDicts, SourcePath: cacheKey}
	refreshLoadedModuleAliases(ctx.loadedModules)
	resolveExternalModels(sourceDir, fileModels, fileCtx)
	ctx.loadedModules[cacheKey] = ModuleSummary{Imports: fileCtx.imports, Models: fileModels, TypedDicts: fileCtx.typedDicts, SourcePath: cacheKey}
	return fileModels
}

func propagateImportedAlias(localName string, entry schema.ImportEntry, models schema.ModelClassMap, typedDicts map[string]bool) {
	propagateImportedAliasFrom(localName, entry, models, models, typedDicts, typedDicts)
}

func propagateImportedAliasFrom(localName string, entry schema.ImportEntry, src schema.ModelClassMap, dst schema.ModelClassMap, srcTypedDicts map[string]bool, dstTypedDicts map[string]bool) {
	if fields, ok := src.Get(entry.Original); ok {
		dst.Set(localName, fields)
	}
	if srcTypedDicts[entry.Original] {
		dstTypedDicts[localName] = true
	}
}

// resolveExternalModels looks at imports that brought in names not yet in
// modelClasses, attempts to find the corresponding .py file on disk, parses
// it, and merges any schema object classes into modelClasses.
//
// This handles local import forms relative to either the project root or the
// importing source file:
//
//	from .types import Output          → <sourceDir>/<source-file-dir>/types.py
//	from types import Output           → <sourceDir>/types.py
//	from models.output import Result   → <sourceDir>/models/output.py
//	from .models.output import Result  → <sourceDir>/<source-file-dir>/models/output.py
//	from my_app.types import Foo       → <sourceDir>/my_app/types.py
//
// Non-local imports (stdlib, pip packages) are skipped because the file
// won't exist on disk.
func resolveExternalModels(sourceDir string, models schema.ModelClassMap, ctx *modelParseContext) {
	ctx.imports.Names.Entries(func(localName string, entry schema.ImportEntry) {
		module, moduleModels, nestedModule := ctx.loadModelsForImport(sourceDir, entry)
		if moduleModels != nil {
			setQualifiedModelAliases(models, localName, moduleModels)
			if pyPath := moduleToFilePath(module, ctx.sourcePath); pyPath != "" {
				if summary, ok := ctx.loadedModules[filepath.Clean(pyPath)]; ok {
					setQualifiedTypedDictAliases(ctx.typedDicts, localName, summary.TypedDicts)
				}
			}
			if nestedModule || entry.Original == entry.Module || entry.Module == "." {
				return
			}
			summaryTypedDicts := ctx.typedDicts
			if pyPath := moduleToFilePath(module, ctx.sourcePath); pyPath != "" {
				if summary, ok := ctx.loadedModules[filepath.Clean(pyPath)]; ok {
					summaryTypedDicts = summary.TypedDicts
				}
			}
			propagateImportedAliasFrom(localName, entry, moduleModels, models, summaryTypedDicts, ctx.typedDicts)
			mergeDiscoveredModels(models, moduleModels)
			return
		}
		propagateImportedAlias(localName, entry, models, ctx.typedDicts)
	})
}

func (ctx *modelParseContext) loadModelsForImport(sourceDir string, entry schema.ImportEntry) (string, schema.ModelClassMap, bool) {
	if models := ctx.loadModelsFromModule(sourceDir, entry.Module); models != nil {
		return entry.Module, models, false
	}
	module := nestedImportModule(entry.Module, entry.Original)
	if module == entry.Module {
		return entry.Module, nil, false
	}
	return module, ctx.loadModelsFromModule(sourceDir, module), true
}

func nestedImportModule(module string, original string) string {
	if module == "" || original == "" {
		return module
	}
	if module == "." {
		return "." + original
	}
	return module + "." + original
}

func refreshLoadedModuleAliases(loadedModules map[string]ModuleSummary) {
	for _, summary := range loadedModules {
		if summary.Imports == nil || summary.Models == nil {
			continue
		}
		summary.Imports.Names.Entries(func(localName string, entry schema.ImportEntry) {
			pyPath := moduleToFilePath(entry.Module, summary.SourcePath)
			if pyPath == "" {
				return
			}
			imported, ok := loadedModules[filepath.Clean(pyPath)]
			if !ok || imported.Models == nil {
				return
			}
			propagateImportedAliasFrom(localName, entry, imported.Models, summary.Models, imported.TypedDicts, summary.TypedDicts)
		})
	}
}

// moduleToFilePath converts a Python module path to a relative .py file path.
//
//	".types", "pkg/predict.py"         → "pkg/types.py"
//	"types", "pkg/predict.py"          → "types.py"
//	".models.output", "pkg/predict.py" → "pkg/models/output.py"
//	"models.output", "pkg/predict.py"  → "models/output.py"
//	"cog", "pkg/predict.py"            → "cog.py"  (known external → skipped)
func moduleToFilePath(module string, sourcePaths ...string) string {
	sourcePath := ""
	if len(sourcePaths) > 0 {
		sourcePath = sourcePaths[0]
	}
	if strings.HasPrefix(module, ".") && sourcePath != "" {
		return relativeModuleToFilePath(module, sourcePath)
	}

	// Strip leading dots (relative import markers).
	clean := strings.TrimLeft(module, ".")
	if clean == "" {
		return ""
	}
	// Replace dots with path separators.
	parts := strings.Split(clean, ".")
	return filepath.Join(parts...) + ".py"
}

func relativeModuleToFilePath(module string, sourcePath string) string {
	level := len(module) - len(strings.TrimLeft(module, "."))
	clean := strings.TrimLeft(module, ".")
	if clean == "" {
		return ""
	}

	baseDir := filepath.Dir(sourcePath)
	if baseDir == "." {
		baseDir = ""
	}
	for range level - 1 {
		if baseDir == "" {
			break
		}
		baseDir = filepath.Dir(baseDir)
		if baseDir == "." {
			baseDir = ""
		}
	}

	parts := strings.Split(clean, ".")
	pathParts := append([]string{}, parts...)
	if baseDir != "" {
		pathParts = append([]string{baseDir}, pathParts...)
	}
	return filepath.Join(pathParts...) + ".py"
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

func InheritsFromBaseModel(classNode *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	supers := classNode.ChildByFieldName("superclasses")
	if supers == nil {
		return false
	}
	for _, child := range AllChildren(supers) {
		switch child.Type() {
		case "identifier":
			name := Content(child, source)
			if imports.IsBaseModel(name) || name == "BaseModel" {
				return true
			}
		case "attribute":
			// Handle dotted access: pydantic.BaseModel, cog.BaseModel
			text := Content(child, source)
			if strings.HasSuffix(text, ".BaseModel") {
				return true
			}
		}
	}
	return false
}

func TypedDictClassInfo(classNode *sitter.Node, source []byte, imports *schema.ImportContext, typedDicts map[string]bool) (bool, bool, []string) {
	supers := classNode.ChildByFieldName("superclasses")
	if supers == nil {
		return false, true, nil
	}

	isTypedDict := false
	requiredByDefault := true
	parents := []string{}
	for _, child := range NamedChildren(supers) {
		switch child.Type() {
		case "identifier":
			name := Content(child, source)
			if imports.IsTypedDict(name) || name == "TypedDict" {
				isTypedDict = true
				continue
			}
			if typedDicts[name] {
				isTypedDict = true
				parents = append(parents, name)
			}
		case "attribute":
			text := Content(child, source)
			if strings.HasSuffix(text, ".TypedDict") {
				isTypedDict = true
				continue
			}
			if typedDicts[text] {
				isTypedDict = true
				parents = append(parents, text)
				continue
			}
			if resolved, _, ok := imports.ResolveQualifiedName(text); ok && !strings.Contains(text, ".") && typedDicts[resolved] {
				isTypedDict = true
				parents = append(parents, resolved)
			}
		case "keyword_argument":
			nameNode := child.ChildByFieldName("name")
			valueNode := child.ChildByFieldName("value")
			if nameNode == nil || valueNode == nil {
				continue
			}
			if Content(nameNode, source) == "total" && strings.TrimSpace(Content(valueNode, source)) == "False" {
				requiredByDefault = false
			}
		}
	}

	return isTypedDict, requiredByDefault, parents
}

func extractClassAnnotations(classNode *sitter.Node, source []byte, imports *schema.ImportContext, isTypedDict bool, requiredByDefault bool) []schema.ModelField {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil
	}

	var fields []schema.ModelField
	for _, child := range NamedChildren(body) {
		node := child
		if child.Type() == "expression_statement" && child.NamedChildCount() == 1 {
			node = child.NamedChild(0)
		}

		switch node.Type() {
		case "assignment":
			if f, ok := parseAnnotatedAssignment(node, source, imports, isTypedDict, requiredByDefault); ok {
				fields = append(fields, f)
			}
		case "type":
			if f, ok := parseBareAnnotation(node, source, imports, isTypedDict, requiredByDefault); ok {
				fields = append(fields, f)
			}
		}
	}
	return fields
}

func parseAnnotatedAssignment(node *sitter.Node, source []byte, imports *schema.ImportContext, isTypedDict bool, requiredByDefault bool) (schema.ModelField, bool) {
	left := node.ChildByFieldName("left")
	typeNode := node.ChildByFieldName("type")
	if left == nil || typeNode == nil || left.Type() != "identifier" {
		return schema.ModelField{}, false
	}

	name := Content(left, source)
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

	typeAnn, keyRequired := typedDictFieldInfo(typeAnn, imports, isTypedDict, requiredByDefault)

	return schema.ModelField{Name: name, Type: typeAnn, Default: def, KeyRequired: keyRequired}, true
}

func parseBareAnnotation(node *sitter.Node, source []byte, imports *schema.ImportContext, isTypedDict bool, requiredByDefault bool) (schema.ModelField, bool) {
	text := strings.TrimSpace(Content(node, source))
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

	typeAnn, keyRequired := typedDictFieldInfo(typeAnn, imports, isTypedDict, requiredByDefault)

	return schema.ModelField{Name: name, Type: typeAnn, Default: nil, KeyRequired: keyRequired}, true
}

func typedDictFieldInfo(typeAnn schema.TypeAnnotation, imports *schema.ImportContext, isTypedDict bool, requiredByDefault bool) (schema.TypeAnnotation, *bool) {
	if !isTypedDict {
		return typeAnn, nil
	}
	if typeAnn.Kind != schema.TypeAnnotGeneric || len(typeAnn.Args) != 1 {
		return typeAnn, boolPtr(requiredByDefault)
	}

	name := typeAnn.Name
	if name == "NotRequired" || strings.HasSuffix(name, ".NotRequired") {
		return typeAnn.Args[0], boolPtr(false)
	}
	if name == "Required" || strings.HasSuffix(name, ".Required") {
		return typeAnn.Args[0], boolPtr(true)
	}
	if imports.IsTypedDictFieldQualifier(name) {
		if entry, ok := imports.Names.Get(name); ok {
			switch entry.Original {
			case "NotRequired":
				return typeAnn.Args[0], boolPtr(false)
			case "Required":
				return typeAnn.Args[0], boolPtr(true)
			}
		}
	}

	return typeAnn, boolPtr(requiredByDefault)
}

func boolPtr(v bool) *bool {
	return &v
}
