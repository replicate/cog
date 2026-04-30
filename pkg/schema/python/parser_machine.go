package python

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"github.com/replicate/cog/pkg/schema"
)

type parsePhase int

const (
	phaseParseSource parsePhase = iota
	phaseCollectModuleFacts
	phaseResolveSchemaObjects
	phaseBuildInputRegistry
	phaseLocateTarget
	phaseExtractInputs
	phaseResolveOutput
	phaseDone
)

type parseState struct {
	phase parsePhase

	source     []byte
	predictRef string
	mode       schema.Mode
	sourceDir  string

	tree *sitter.Tree
	root *sitter.Node

	facts moduleFacts

	modelCtx      *modelParseContext
	modelClasses  schema.ModelClassMap
	inputRegistry *inputRegistry

	methodName string
	funcNode   *sitter.Node
	paramsNode *sitter.Node
	isMethod   bool

	inputs *schema.OrderedMap[string, schema.InputField]
	output schema.SchemaType
}

type moduleFacts struct {
	imports   *schema.ImportContext
	scope     moduleScope
	classes   []classDef
	functions []functionDef
}

type classDef struct {
	name string
	node *sitter.Node
}

type functionDef struct {
	name string
	node *sitter.Node
}

func newParseState(source []byte, predictRef string, mode schema.Mode, sourceDir string) *parseState {
	methodName := "predict"
	if mode == schema.ModeTrain {
		methodName = "train"
	}
	return &parseState{
		phase:      phaseParseSource,
		source:     source,
		predictRef: predictRef,
		mode:       mode,
		sourceDir:  sourceDir,
		methodName: methodName,
	}
}

func (st *parseState) step() error {
	switch st.phase {
	case phaseParseSource:
		return st.stepParseSource()
	case phaseCollectModuleFacts:
		return st.stepCollectModuleFacts()
	case phaseResolveSchemaObjects:
		return st.stepResolveSchemaObjects()
	case phaseBuildInputRegistry:
		return st.stepBuildInputRegistry()
	case phaseLocateTarget:
		return st.stepLocateTarget()
	case phaseExtractInputs:
		return st.stepExtractInputs()
	case phaseResolveOutput:
		return st.stepResolveOutput()
	case phaseDone:
		return nil
	default:
		return schema.WrapError(schema.ErrParse, fmt.Sprintf("unknown parse phase %d", st.phase), nil)
	}
}

func (st *parseState) stepParseSource() error {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())

	tree, err := parser.ParseCtx(context.Background(), nil, st.source)
	if err != nil {
		return schema.WrapError(schema.ErrParse, "tree-sitter parse failed", err)
	}

	st.tree = tree
	st.root = tree.RootNode()
	st.phase = phaseCollectModuleFacts
	return nil
}

func (st *parseState) stepCollectModuleFacts() error {
	st.facts = collectModuleFacts(st.root, st.source)
	st.phase = phaseResolveSchemaObjects
	return nil
}

func (st *parseState) stepResolveSchemaObjects() error {
	st.modelCtx = &modelParseContext{imports: st.facts.imports, typedDicts: make(map[string]bool)}
	st.modelClasses = collectModelClasses(st.facts.classes, st.source, st.modelCtx)
	if st.sourceDir != "" {
		resolveExternalModels(st.sourceDir, st.modelClasses, st.modelCtx)
	}
	st.phase = phaseBuildInputRegistry
	return nil
}

func (st *parseState) stepBuildInputRegistry() error {
	st.inputRegistry = collectInputRegistry(st.facts.classes, st.source, st.facts.imports, st.facts.scope)
	st.phase = phaseLocateTarget
	return nil
}

func (st *parseState) stepLocateTarget() error {
	funcNode, err := findTargetFunction(st.facts, st.source, st.predictRef, st.methodName)
	if err != nil {
		return err
	}
	paramsNode := funcNode.ChildByFieldName("parameters")
	if paramsNode == nil {
		return schema.WrapError(schema.ErrParse, "function has no parameters node", nil)
	}
	st.funcNode = funcNode
	st.paramsNode = paramsNode
	st.isMethod = firstParamIsSelf(paramsNode, st.source)
	st.phase = phaseExtractInputs
	return nil
}

func (st *parseState) stepExtractInputs() error {
	paramCtx := &inputParseContext{
		methodName: st.methodName,
		imports:    st.facts.imports,
		registry:   st.inputRegistry,
		scope:      st.facts.scope,
		typedDicts: st.modelCtx.typedDicts,
	}
	inputs, err := extractInputs(st.paramsNode, st.source, st.isMethod, paramCtx)
	if err != nil {
		return err
	}
	st.inputs = inputs
	st.phase = phaseResolveOutput
	return nil
}

func (st *parseState) stepResolveOutput() error {
	returnAnn := st.funcNode.ChildByFieldName("return_type")
	if returnAnn == nil {
		return schema.WrapError(schema.ErrMissingReturnType, st.methodName, nil)
	}
	returnTypeAnn, err := parseTypeAnnotation(returnAnn, st.source)
	if err != nil {
		return err
	}
	output, err := schema.ResolveSchemaType(returnTypeAnn, st.facts.imports, st.modelClasses)
	if err != nil {
		return err
	}
	st.output = output
	st.phase = phaseDone
	return nil
}

func (st *parseState) result() *schema.PredictorInfo {
	return &schema.PredictorInfo{
		Inputs: st.inputs,
		Output: st.output,
		Mode:   st.mode,
	}
}

func collectModuleFacts(root *sitter.Node, source []byte) moduleFacts {
	return moduleFacts{
		imports:   CollectImports(root, source),
		scope:     collectModuleScope(root, source),
		classes:   collectTopLevelClasses(root, source),
		functions: collectTopLevelFunctions(root, source),
	}
}

func collectTopLevelClasses(root *sitter.Node, source []byte) []classDef {
	classes := []classDef{}
	for _, child := range NamedChildren(root) {
		classNode := UnwrapClass(child)
		if classNode == nil {
			continue
		}
		nameNode := classNode.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		classes = append(classes, classDef{name: Content(nameNode, source), node: classNode})
	}
	return classes
}

func collectTopLevelFunctions(root *sitter.Node, source []byte) []functionDef {
	functions := []functionDef{}
	for _, child := range NamedChildren(root) {
		funcNode := UnwrapFunction(child)
		if funcNode == nil {
			continue
		}
		nameNode := funcNode.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		functions = append(functions, functionDef{name: Content(nameNode, source), node: funcNode})
	}
	return functions
}
