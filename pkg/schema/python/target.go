package python

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/replicate/cog/pkg/schema"
)

type pythonFileContext struct {
	root          *sitter.Node
	source        []byte
	imports       *schema.ImportContext
	moduleScope   moduleScope
	inputRegistry *inputRegistry
	modelClasses  schema.ModelClassMap
	typedDicts    map[string]bool
	sourceDir     string
	mode          schema.Mode
	fileCache     map[string]*pythonFileContext
	loading       map[string]bool
}

type targetFunction struct {
	node *sitter.Node
	file *pythonFileContext
}

func targetMethodNameForMode(mode schema.Mode) string {
	if mode == schema.ModeTrain {
		return "train"
	}
	return "predict"
}

func findTargetCallableNode(file *pythonFileContext, targetRef, methodName string) (*targetFunction, error) {
	for _, child := range NamedChildren(file.root) {
		classNode := UnwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode != nil && Content(nameNode, file.source) == targetRef {
			if file.mode == schema.ModePredict {
				return findPredictMethodInClass(file, classNode, targetRef)
			}
			method, err := findNamedMethodInClass(classNode, file.source, targetRef, methodName)
			if err != nil {
				return nil, err
			}
			return &targetFunction{node: method, file: file}, nil
		}
	}

	for _, child := range NamedChildren(file.root) {
		funcNode := UnwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode != nil {
			name := Content(nameNode, file.source)
			if name == targetRef || name == methodName {
				return &targetFunction{node: funcNode, file: file}, nil
			}
		}
	}

	return nil, schema.WrapError(schema.ErrPredictorNotFound, targetRef, nil)
}

func findPredictMethodInClass(file *pythonFileContext, classNode *sitter.Node, className string) (*targetFunction, error) {
	runNode, predictNode := collectPredictMethods(file, classNode, className, map[string]bool{})
	if runNode != nil {
		return runNode, nil
	}
	if predictNode != nil {
		fmt.Fprintf(os.Stderr, "cog: warning: %s.predict() is deprecated; use run() instead\n", className)
		return predictNode, nil
	}
	return nil, schema.WrapError(schema.ErrMethodNotFound, fmt.Sprintf("%s must define run() or predict()", className), nil)
}

func collectPredictMethods(file *pythonFileContext, classNode *sitter.Node, className string, seen map[string]bool) (*targetFunction, *targetFunction) {
	seenKey := fmt.Sprintf("%p:%s", file.root, className)
	if seen[seenKey] {
		return nil, nil
	}
	seen[seenKey] = true

	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil, nil
	}

	var runNode *targetFunction
	var predictNode *targetFunction
	for _, child := range NamedChildren(body) {
		funcNode := UnwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		switch Content(nameNode, file.source) {
		case "run":
			runNode = &targetFunction{node: funcNode, file: file}
		case "predict":
			predictNode = &targetFunction{node: funcNode, file: file}
		}
	}

	for _, parent := range classParentNames(classNode, file.source) {
		parentFile := file
		parentClass := parent
		parentNode := findClassByName(file.root, file.source, parent)
		if parentNode == nil {
			var ok bool
			parentFile, parentNode, parentClass, ok = resolveImportedParentClass(file, parent)
			if !ok {
				continue
			}
		}
		if parentNode == nil {
			continue
		}
		parentRun, parentPredict := collectPredictMethods(parentFile, parentNode, parentClass, seen)
		if runNode == nil {
			runNode = parentRun
		}
		if predictNode == nil {
			predictNode = parentPredict
		}
	}
	return runNode, predictNode
}

func resolveImportedParentClass(file *pythonFileContext, parent string) (*pythonFileContext, *sitter.Node, string, bool) {
	if file.sourceDir == "" {
		return nil, nil, "", false
	}

	var module, className string
	if before, after, ok := strings.Cut(parent, "."); ok {
		entry, ok := file.imports.Names.Get(before)
		if !ok {
			return nil, nil, "", false
		}
		module = entry.Module
		className = after
	} else if entry, ok := file.imports.Names.Get(parent); ok {
		module = entry.Module
		className = entry.Original
	} else {
		return nil, nil, "", false
	}

	parentFile, ok := loadPythonFileContext(file.sourceDir, module, file.mode, file.fileCache, file.loading)
	if !ok {
		return nil, nil, "", false
	}
	parentNode := findClassByName(parentFile.root, parentFile.source, className)
	return parentFile, parentNode, className, parentNode != nil
}

func loadPythonFileContext(sourceDir, module string, mode schema.Mode, fileCache map[string]*pythonFileContext, loading map[string]bool) (*pythonFileContext, bool) {
	if isKnownExternalModule(module) {
		return nil, false
	}
	pyPath := moduleToFilePath(module)
	if pyPath == "" {
		return nil, false
	}
	fullPath := filepath.Clean(filepath.Join(sourceDir, pyPath))
	if file, ok := fileCache[fullPath]; ok {
		return file, true
	}
	if loading[fullPath] {
		return nil, false
	}
	loading[fullPath] = true
	defer delete(loading, fullPath)

	source, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, false
	}
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, false
	}
	root := tree.RootNode()
	imports := CollectImports(root, source)
	moduleScope := collectModuleScope(root, source)
	modelCtx := &modelParseContext{imports: imports, typedDicts: make(map[string]bool), loadedModules: make(map[string]ModuleSummary)}
	modelClasses := collectModelClasses(root, source, modelCtx)
	resolveExternalModels(sourceDir, modelClasses, modelCtx)
	inputRegistry := collectInputRegistry(root, source, imports, moduleScope)
	fileCtx := &pythonFileContext{
		root:          root,
		source:        source,
		imports:       imports,
		moduleScope:   moduleScope,
		inputRegistry: inputRegistry,
		modelClasses:  modelClasses,
		typedDicts:    modelCtx.typedDicts,
		sourceDir:     sourceDir,
		mode:          mode,
		fileCache:     fileCache,
		loading:       loading,
	}
	fileCache[fullPath] = fileCtx
	return fileCtx, true
}

func findClassByName(root *sitter.Node, source []byte, name string) *sitter.Node {
	for _, child := range NamedChildren(root) {
		classNode := UnwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode != nil && Content(nameNode, source) == name {
			return classNode
		}
	}
	return nil
}

func classParentNames(classNode *sitter.Node, source []byte) []string {
	supers := classNode.ChildByFieldName("superclasses")
	if supers == nil {
		return nil
	}
	parents := []string{}
	for _, child := range NamedChildren(supers) {
		if child.Type() == "identifier" || child.Type() == "attribute" {
			parents = append(parents, Content(child, source))
		}
	}
	return parents
}

func findNamedMethodInClass(classNode *sitter.Node, source []byte, className, methodName string) (*sitter.Node, error) {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return nil, schema.WrapError(schema.ErrParse, fmt.Sprintf("class %s has no body", className), nil)
	}

	for _, child := range NamedChildren(body) {
		funcNode := UnwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode != nil && Content(nameNode, source) == methodName {
			return funcNode, nil
		}
	}

	return nil, schema.WrapError(schema.ErrMethodNotFound, fmt.Sprintf("%s.%s not found", className, methodName), nil)
}
