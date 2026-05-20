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

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/replicate/cog/pkg/schema"
)

// ParsePredictor parses Python source and extracts predictor information.
// predictRef is the class or function name (e.g. "Predictor" or "predict").
// mode controls whether we look for predict or train method.
// sourceDir is the project root for resolving cross-file imports. Pass "" if unknown.
func ParsePredictor(source []byte, predictRef string, mode schema.Mode, sourceDir string) (*schema.PredictorInfo, error) {
	return ParseWithOptions(defaultParserOptions(source, predictRef, mode, sourceDir))
}

func ParseWithOptions(opts ParserOptions) (*schema.PredictorInfo, error) {
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
	return &schema.PredictorInfo{Inputs: state.Inputs, Output: state.Output, Mode: opts.Mode}, nil
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
	state.ModelCtx = &modelParseContext{imports: state.Imports, typedDicts: make(map[string]bool), loadedModules: state.LoadedModules}
	state.Models = collectModelClasses(state.Root, state.Options.Source, state.ModelCtx)
	return nil
}

func resolveImportedModelsPhase(state *ParseState) error {
	if err := state.requirePhase(phaseLocalModelsCollected); err != nil {
		return err
	}
	if state.Options.SourceDir != "" {
		resolveExternalModels(state.Options.SourceDir, state.Models, state.ModelCtx)
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
	methodName := "predict"
	if state.Options.Mode == schema.ModeTrain {
		methodName = "train"
	}
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
		fileCache:     make(map[string]*pythonFileContext),
		loading:       make(map[string]bool),
	}
	target, err := findTargetFunction(fileCtx, state.Options.TargetRef, methodName)
	if err != nil {
		return err
	}
	state.FileCtx = fileCtx
	state.TargetFunc = target
	return nil
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
	state.Output = output
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
