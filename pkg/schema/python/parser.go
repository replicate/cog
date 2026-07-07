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
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/replicate/cog/pkg/schema"
)

// ParsePredictor parses Python source and extracts predictor information.
// targetRef is the class or function name (e.g. "Predictor" or "run").
// mode controls whether we look for predict or train method.
// sourceDir is the project root for resolving cross-file imports. Pass "" if unknown.
func ParsePredictor(source []byte, targetRef string, mode schema.Mode, sourceDir string) (*schema.PredictorInfo, error) {
	return ParsePredictorWithSourcePath(source, targetRef, mode, sourceDir, "")
}

// ParsePredictorWithSourcePath parses Python source and uses sourcePath,
// relative to sourceDir, to resolve package-relative imports.
func ParsePredictorWithSourcePath(source []byte, targetRef string, mode schema.Mode, sourceDir string, sourcePath string) (*schema.PredictorInfo, error) {
	opts := defaultParserOptions(source, targetRef, mode, sourceDir)
	opts.SourcePath = sourcePath
	return ParseWithOptions(opts)
}

func ParsePredictorMetadataWithSourcePath(source []byte, targetRef string, mode schema.Mode, sourceDir string, sourcePath string) (*schema.PredictorInfo, error) {
	opts := defaultParserOptions(source, targetRef, mode, sourceDir)
	opts.SourcePath = sourcePath
	return ParseMetadataWithOptions(opts)
}

func ParseMetadataWithOptions(opts ParserOptions) (*schema.PredictorInfo, error) {
	sourcePath, err := normalizeSourcePath(opts.SourceDir, opts.SourcePath)
	if err != nil {
		return nil, err
	}
	opts.SourcePath = sourcePath
	state := newParseState(opts)
	phases := []parserPhase{
		{Name: "parse module", From: phaseInitial, To: phaseModuleParsed, Run: parseModulePhase},
		{Name: "collect imports", From: phaseModuleParsed, To: phaseImportsCollected, Run: collectImportsPhase},
		{Name: "collect module scope", From: phaseImportsCollected, To: phaseModuleScopeCollected, Run: collectModuleScopePhase},
		{Name: "collect local models", From: phaseModuleScopeCollected, To: phaseLocalModelsCollected, Run: collectLocalModelsPhase},
		{Name: "resolve imported models", From: phaseLocalModelsCollected, To: phaseImportedModelsResolved, Run: resolveImportedModelsPhase},
		{Name: "collect input registry", From: phaseImportedModelsResolved, To: phaseInputRegistryCollected, Run: collectInputRegistryPhase},
		{Name: "find target callable", From: phaseInputRegistryCollected, To: phaseTargetFound, Run: findTargetCallablePhase},
	}
	if err := runPhases(state, phases); err != nil {
		return nil, err
	}
	targetSource := state.TargetFunc.file.source
	isAsync := functionIsAsync(state.TargetFunc.node, targetSource)
	concurrencyMax, err := functionConcurrencyMax(state.TargetFunc.node, targetSource, state.TargetFunc.file.imports)
	if err != nil {
		return nil, err
	}
	if concurrencyMax != nil && *concurrencyMax > 1 && !isAsync {
		return nil, schema.WrapError(schema.ErrUnsupportedType, "@concurrent(max > 1) requires an async run() or predict() method", nil)
	}
	return &schema.PredictorInfo{
		Mode:           opts.Mode,
		ConcurrencyMax: concurrencyMax,
		IsAsync:        isAsync,
	}, nil
}

func ParseWithOptions(opts ParserOptions) (*schema.PredictorInfo, error) {
	sourcePath, err := normalizeSourcePath(opts.SourceDir, opts.SourcePath)
	if err != nil {
		return nil, err
	}
	opts.SourcePath = sourcePath
	state := newParseState(opts)
	phases := []parserPhase{
		{Name: "parse module", From: phaseInitial, To: phaseModuleParsed, Run: parseModulePhase},
		{Name: "collect imports", From: phaseModuleParsed, To: phaseImportsCollected, Run: collectImportsPhase},
		{Name: "collect module scope", From: phaseImportsCollected, To: phaseModuleScopeCollected, Run: collectModuleScopePhase},
		{Name: "collect local models", From: phaseModuleScopeCollected, To: phaseLocalModelsCollected, Run: collectLocalModelsPhase},
		{Name: "resolve imported models", From: phaseLocalModelsCollected, To: phaseImportedModelsResolved, Run: resolveImportedModelsPhase},
		{Name: "collect input registry", From: phaseImportedModelsResolved, To: phaseInputRegistryCollected, Run: collectInputRegistryPhase},
		{Name: "find target callable", From: phaseInputRegistryCollected, To: phaseTargetFound, Run: findTargetCallablePhase},
		{Name: "extract inputs", From: phaseTargetFound, To: phaseInputsExtracted, Run: extractInputsPhase},
		{Name: "resolve output", From: phaseInputsExtracted, To: phaseOutputResolved, Run: resolveOutputPhase},
		{Name: "build runner info", From: phaseOutputResolved, To: phaseBuilt, Run: buildRunnerInfoPhase},
	}
	if err := runPhases(state, phases); err != nil {
		return nil, err
	}
	return &schema.PredictorInfo{
		Inputs:            state.Inputs,
		Output:            state.Output,
		Mode:              opts.Mode,
		SupportsStreaming: state.SupportsStreaming,
		ConcurrencyMax:    state.ConcurrencyMax,
		IsAsync:           state.IsAsync,
	}, nil
}

func parseModulePhase(state *ParseState) error {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, state.Options.Source)
	if err != nil {
		return schema.WrapError(schema.ErrParse, "tree-sitter parse failed", err)
	}
	state.Tree = tree
	state.Root = tree.RootNode()
	return nil
}

func collectImportsPhase(state *ParseState) error {
	if err := state.requirePhase(phaseModuleParsed); err != nil {
		return err
	}
	state.Imports = CollectImports(state.Root, state.Options.Source)
	return nil
}

func collectModuleScopePhase(state *ParseState) error {
	if err := state.requirePhase(phaseImportsCollected); err != nil {
		return err
	}
	state.Scope = collectModuleScope(state.Root, state.Options.Source)
	return nil
}

func collectLocalModelsPhase(state *ParseState) error {
	if err := state.requirePhase(phaseModuleScopeCollected); err != nil {
		return err
	}
	state.ModelCtx = &modelParseContext{imports: state.Imports, typedDicts: make(map[string]bool), loadedModules: state.LoadedModules, sourcePath: state.Options.SourcePath}
	state.Models = collectModelClasses(state.Root, state.Options.Source, state.ModelCtx)
	state.ModelCtx.resolvedModels = state.Models
	return nil
}

func resolveImportedModelsPhase(state *ParseState) error {
	if err := state.requirePhase(phaseLocalModelsCollected); err != nil {
		return err
	}
	if state.Options.SourceDir != "" {
		resolveExternalModels(state.Options.SourceDir, state.Models, state.ModelCtx)
		state.ModelCtx.resolvedModels = state.Models
		setDiscoveredModels(state.Models, collectModelClasses(state.Root, state.Options.Source, state.ModelCtx))
	}
	return nil
}

func collectInputRegistryPhase(state *ParseState) error {
	if err := state.requirePhase(phaseImportedModelsResolved); err != nil {
		return err
	}
	state.Registry = collectInputRegistry(state.Root, state.Options.Source, state.Imports, state.Scope)
	return nil
}

func findTargetCallablePhase(state *ParseState) error {
	if err := state.requirePhase(phaseInputRegistryCollected); err != nil {
		return err
	}
	methodName := targetMethodNameForMode(state.Options.Mode)
	fileCtx := &pythonFileContext{
		root:          state.Root,
		source:        state.Options.Source,
		imports:       state.Imports,
		moduleScope:   state.Scope,
		inputRegistry: state.Registry,
		modelClasses:  state.Models,
		typedDicts:    state.ModelCtx.typedDicts,
		sourceDir:     state.Options.SourceDir,
		mode:          state.Options.Mode,
		sourcePath:    state.Options.SourcePath,
		allowLegacy:   !state.Options.DisableLegacyPredict,
		fileCache:     make(map[string]*pythonFileContext),
		loading:       make(map[string]bool),
	}
	target, err := findTargetCallableNode(fileCtx, state.Options.TargetRef, methodName)
	if err != nil {
		return err
	}
	state.FileCtx = fileCtx
	state.TargetFunc = target
	return nil
}

func normalizeSourcePath(sourceDir string, sourcePath string) (string, error) {
	if sourcePath == "" {
		return "", nil
	}
	if !filepath.IsAbs(sourcePath) || sourceDir == "" {
		clean := filepath.Clean(sourcePath)
		if filepath.IsAbs(clean) || pathEscapesSourceDir(clean) {
			return "", schema.WrapError(schema.ErrParse, fmt.Sprintf("source path %q is outside source directory", sourcePath), nil)
		}
		return clean, nil
	}
	rel, err := filepath.Rel(sourceDir, sourcePath)
	if err != nil || pathEscapesSourceDir(rel) || filepath.IsAbs(rel) {
		return "", schema.WrapError(schema.ErrParse, fmt.Sprintf("source path %q is outside source directory", sourcePath), nil)
	}
	return filepath.Clean(rel), nil
}

func pathEscapesSourceDir(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func extractInputsPhase(state *ParseState) error {
	if err := state.requirePhase(phaseTargetFound); err != nil {
		return err
	}
	funcNode := state.TargetFunc.node
	targetSource := state.TargetFunc.file.source
	actualMethodName := "predict"
	if state.Options.Mode == schema.ModeTrain {
		actualMethodName = "train"
	}
	if nameNode := funcNode.ChildByFieldName("name"); nameNode != nil {
		actualMethodName = Content(nameNode, targetSource)
	}

	paramsNode := funcNode.ChildByFieldName("parameters")
	if paramsNode == nil {
		return schema.WrapError(schema.ErrParse, "function has no parameters node", nil)
	}
	isMethod := firstParamIsSelf(paramsNode, targetSource)

	paramCtx := &inputParseContext{
		methodName: actualMethodName,
		imports:    state.TargetFunc.file.imports,
		registry:   state.TargetFunc.file.inputRegistry,
		scope:      state.TargetFunc.file.moduleScope,
		typedDicts: state.TargetFunc.file.typedDicts,
	}
	inputs, err := extractInputs(paramsNode, targetSource, isMethod, paramCtx)
	if err != nil {
		return err
	}
	state.Target = &TargetCallable{MethodName: actualMethodName, Node: funcNode, IsMethod: isMethod}
	state.Inputs = inputs
	state.IsAsync = functionIsAsync(funcNode, targetSource)
	return nil
}

func resolveOutputPhase(state *ParseState) error {
	if err := state.requirePhase(phaseInputsExtracted); err != nil {
		return err
	}
	returnAnn := state.TargetFunc.node.ChildByFieldName("return_type")
	if returnAnn == nil {
		return schema.WrapError(schema.ErrMissingReturnType, state.Target.MethodName, nil)
	}
	returnTypeAnn, err := parseTypeAnnotation(returnAnn, state.TargetFunc.file.source)
	if err != nil {
		return err
	}
	output, err := schema.ResolveSchemaType(returnTypeAnn, state.TargetFunc.file.imports, state.TargetFunc.file.modelClasses)
	if err != nil {
		return err
	}
	supportsStreaming := functionSupportsStreaming(state.TargetFunc.node, state.TargetFunc.file.source, state.TargetFunc.file.imports)
	if supportsStreaming && !supportsStreamingOutput(output) {
		return schema.WrapError(schema.ErrUnsupportedType, "@streaming requires Iterator[...], AsyncIterator[...], ConcatenateIterator[...] or AsyncConcatenateIterator[...] return type", nil)
	}
	concurrencyMax, err := functionConcurrencyMax(state.TargetFunc.node, state.TargetFunc.file.source, state.TargetFunc.file.imports)
	if err != nil {
		return err
	}
	if concurrencyMax != nil && *concurrencyMax > 1 && !state.IsAsync {
		return schema.WrapError(schema.ErrUnsupportedType, "@concurrent(max > 1) requires an async run() or predict() method", nil)
	}
	state.Output = output
	state.SupportsStreaming = supportsStreaming
	state.ConcurrencyMax = concurrencyMax
	state.OutputSet = true
	return nil
}

func buildRunnerInfoPhase(state *ParseState) error {
	if err := state.requirePhase(phaseOutputResolved); err != nil {
		return err
	}
	if state.Inputs == nil || !state.OutputSet {
		return schema.WrapError(schema.ErrParse, "parser reached build phase without inputs or output", nil)
	}
	return nil
}
