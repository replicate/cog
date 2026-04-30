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
	imports    *schema.ImportContext
	typedDicts map[string]bool
}

func collectModelClasses(classes []classDef, source []byte, ctx *modelParseContext) schema.ModelClassMap {
	models := schema.NewOrderedMap[string, []schema.ModelField]()

	for _, classDef := range classes {
		classNode := classDef.node
		className := classDef.name
		isBaseModel := InheritsFromBaseModel(classNode, source, ctx.imports)
		isTypedDict, requiredByDefault, parents := TypedDictClassInfo(classNode, source, ctx.imports, ctx.typedDicts)
		if !isBaseModel && !isTypedDict {
			continue
		}

		fields := extractClassAnnotations(classNode, source, ctx.imports, isTypedDict, requiredByDefault)
		if isTypedDict {
			fields = mergeModelFields(parentModelFields(models, parents), fields)
		}
		models.Set(className, fields)
		if isTypedDict {
			ctx.typedDicts[className] = true
		}
	}
	return models
}

func parentModelFields(models schema.ModelClassMap, parents []string) [][]schema.ModelField {
	merged := make([][]schema.ModelField, 0, len(parents))
	for _, parent := range parents {
		if fields, ok := models.Get(parent); ok {
			merged = append(merged, fields)
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

func (ctx *modelParseContext) loadModelsFromModule(sourceDir, module string) schema.ModelClassMap {
	if isKnownExternalModule(module) {
		return nil
	}

	pyPath := moduleToFilePath(module)
	if pyPath == "" {
		return nil
	}

	fullPath := filepath.Join(sourceDir, pyPath)
	source, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "cog: warning: failed to read %q: %v\n", fullPath, err)
		return nil
	}

	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cog: warning: failed to parse %q: %v\n", fullPath, err)
		return nil
	}

	fileFacts := collectModuleFacts(tree.RootNode(), source)
	fileCtx := &modelParseContext{imports: fileFacts.imports, typedDicts: make(map[string]bool)}
	fileModels := collectModelClasses(fileFacts.classes, source, fileCtx)
	for name := range fileCtx.typedDicts {
		ctx.typedDicts[name] = true
	}
	return fileModels
}

func propagateImportedAlias(localName string, entry schema.ImportEntry, models schema.ModelClassMap, typedDicts map[string]bool) {
	if localName == entry.Original {
		return
	}
	if fields, ok := models.Get(entry.Original); ok {
		if _, exists := models.Get(localName); !exists {
			models.Set(localName, fields)
		}
	}
	if typedDicts[entry.Original] {
		typedDicts[localName] = true
	}
}

func resolveExternalModels(sourceDir string, models schema.ModelClassMap, ctx *modelParseContext) {
	tried := make(map[string]bool)

	ctx.imports.Names.Entries(func(localName string, entry schema.ImportEntry) {
		if _, ok := models.Get(localName); ok {
			return
		}

		module := entry.Module
		if !tried[module] {
			tried[module] = true
			mergeDiscoveredModels(models, ctx.loadModelsFromModule(sourceDir, module))
		}

		propagateImportedAlias(localName, entry, models, ctx.typedDicts)
	})
}

func moduleToFilePath(module string) string {
	clean := strings.TrimLeft(module, ".")
	if clean == "" {
		return ""
	}
	parts := strings.Split(clean, ".")
	return filepath.Join(parts...) + ".py"
}

func isKnownExternalModule(module string) bool {
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
			if resolved, _, ok := imports.ResolveQualifiedName(text); ok && typedDicts[resolved] {
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
		return typeAnn, ptr(requiredByDefault)
	}

	name := typeAnn.Name
	if name == "NotRequired" || strings.HasSuffix(name, ".NotRequired") {
		return typeAnn.Args[0], ptr(false)
	}
	if name == "Required" || strings.HasSuffix(name, ".Required") {
		return typeAnn.Args[0], ptr(true)
	}
	if imports.IsTypedDictFieldQualifier(name) {
		if entry, ok := imports.Names.Get(name); ok {
			switch entry.Original {
			case "NotRequired":
				return typeAnn.Args[0], ptr(false)
			case "Required":
				return typeAnn.Args[0], ptr(true)
			}
		}
	}

	return typeAnn, ptr(requiredByDefault)
}
