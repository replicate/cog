package python

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

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
	Accept      *string
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

		body := classNode.ChildByFieldName("body")
		if body == nil {
			continue
		}

		for _, stmt := range NamedChildren(body) {
			inner := stmt
			if stmt.Type() == "expression_statement" && stmt.NamedChildCount() == 1 {
				inner = stmt.NamedChild(0)
			}

			if inner.Type() == "assignment" {
				collectInputAttribute(className, inner, source, imports, scope, registry)
			}

			if funcNode := UnwrapFunction(inner); funcNode != nil {
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
	attrName := Content(left, source)

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
	methodName := Content(nameNode, source)

	params := funcNode.ChildByFieldName("parameters")
	if params == nil {
		return
	}

	var paramNames []string
	for _, param := range AllChildren(params) {
		switch param.Type() {
		case "identifier":
			name := Content(param, source)
			if name != "self" && name != "cls" {
				paramNames = append(paramNames, name)
			}
		case "typed_parameter":
			// typed_parameter has no "name" field; first identifier child is the name
			for j := 0; j < int(param.NamedChildCount()); j++ {
				c := param.NamedChild(j)
				if c.Type() == "identifier" {
					name := Content(c, source)
					if name != "self" && name != "cls" {
						paramNames = append(paramNames, name)
					}
					break
				}
			}
		case "typed_default_parameter", "default_parameter":
			if n := param.ChildByFieldName("name"); n != nil {
				name := Content(n, source)
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
	for _, child := range NamedChildren(body) {
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
		text := Content(node, source)
		info, ok := registry.Attributes[text]
		return info, ok

	case "call":
		funcNode := node.ChildByFieldName("function")
		if funcNode == nil || funcNode.Type() != "attribute" {
			return inputCallInfo{}, false
		}
		key := Content(funcNode, source)
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
		for _, arg := range NamedChildren(args) {
			if arg.Type() == "keyword_argument" {
				nameNode := arg.ChildByFieldName("name")
				valNode := arg.ChildByFieldName("value")
				if nameNode != nil && valNode != nil {
					argValues[Content(nameNode, source)] = valNode
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

type inputParseContext struct {
	methodName string
	imports    *schema.ImportContext
	registry   *inputRegistry
	scope      moduleScope
	typedDicts map[string]bool
}

func typedParameterParts(node *sitter.Node, source []byte) (string, *sitter.Node) {
	var name string
	var typeNode *sitter.Node
	for i := 0; i < int(node.NamedChildCount()); i++ {
		c := node.NamedChild(i)
		switch c.Type() {
		case "identifier":
			if name == "" {
				name = Content(c, source)
			}
		case "type":
			typeNode = c
		}
	}
	return name, typeNode
}

func inputField(name string, order int, inputType schema.InputType, fieldType schema.FieldType) schema.InputField {
	return schema.InputField{
		Name:      name,
		Order:     order,
		FieldType: fieldType,
		InputType: &inputType,
	}
}

func inputFieldWithInfo(name string, order int, inputType schema.InputType, fieldType schema.FieldType, info inputCallInfo) (schema.InputField, error) {
	if info.Accept != nil && fieldType.Primitive != schema.TypePath && fieldType.Primitive != schema.TypeFile {
		return schema.InputField{}, schema.NewError(schema.ErrAcceptOnNonFileType,
			fmt.Sprintf("accept is only valid on Path or File inputs (parameter '%s')", name))
	}
	field := inputField(name, order, inputType, fieldType)
	field.Default = info.Default
	field.Description = info.Description
	field.GE = info.GE
	field.LE = info.LE
	field.MinLength = info.MinLength
	field.MaxLength = info.MaxLength
	field.Regex = info.Regex
	field.Choices = info.Choices
	field.Deprecated = info.Deprecated
	field.Accept = info.Accept
	return field, nil
}

func firstParamIsSelf(params *sitter.Node, source []byte) bool {
	for _, child := range AllChildren(params) {
		if child.Type() == "identifier" {
			return Content(child, source) == "self"
		}
	}
	return false
}

func resolveParameterInputTypes(typeNode *sitter.Node, source []byte, ctx *inputParseContext) (schema.InputType, schema.FieldType, error) {
	typeAnn, err := parseTypeAnnotation(typeNode, source)
	if err != nil {
		return schema.InputType{}, schema.FieldType{}, err
	}
	return schema.ResolveInputType(typeAnn, ctx.imports, ctx.typedDicts)
}

func extractInputs(
	paramsNode *sitter.Node,
	source []byte,
	skipSelf bool,
	ctx *inputParseContext,
) (*schema.OrderedMap[string, schema.InputField], error) {
	inputs := schema.NewOrderedMap[string, schema.InputField]()
	order := 0
	seenSelf := false

	for _, child := range AllChildren(paramsNode) {
		switch child.Type() {
		case "identifier":
			if !seenSelf && skipSelf {
				name := Content(child, source)
				if name == "self" {
					seenSelf = true
					continue
				}
			}

		case "typed_parameter":
			input, err := parseTypedParameter(child, source, order, ctx)
			if err != nil {
				return nil, err
			}
			inputs.Set(input.Name, input)
			order++

		case "typed_default_parameter":
			input, err := parseTypedDefaultParameter(child, source, order, ctx)
			if err != nil {
				return nil, err
			}
			inputs.Set(input.Name, input)
			order++

		case "default_parameter":
			nameNode := child.ChildByFieldName("name")
			paramName := "<unknown>"
			if nameNode != nil {
				paramName = Content(nameNode, source)
			}
			return nil, schema.WrapError(schema.ErrMissingTypeAnnotation, fmt.Sprintf("parameter '%s' on %s has no type annotation", paramName, ctx.methodName), nil)
		}
	}

	return inputs, nil
}

func parseTypedParameter(node *sitter.Node, source []byte, order int, ctx *inputParseContext) (schema.InputField, error) {
	// typed_parameter has no "name" field in the Python grammar.
	// Structure is: identifier ":" type
	name, typeNode := typedParameterParts(node, source)
	if name == "" {
		return schema.InputField{}, schema.WrapError(schema.ErrParse, "typed_parameter has no identifier", nil)
	}
	if typeNode == nil {
		return schema.InputField{}, schema.WrapError(schema.ErrMissingTypeAnnotation, fmt.Sprintf("parameter '%s' on %s has no type annotation", name, ctx.methodName), nil)
	}

	inputType, fieldType, err := resolveParameterInputTypes(typeNode, source, ctx)
	if err != nil {
		return schema.InputField{}, err
	}

	field := inputField(name, order, inputType, fieldType)
	return field, schema.ValidateInputField(field)
}

func parseTypedDefaultParameter(node *sitter.Node, source []byte, order int, ctx *inputParseContext) (schema.InputField, error) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return schema.InputField{}, schema.WrapError(schema.ErrParse, "typed_default_parameter has no name", nil)
	}
	name := Content(nameNode, source)

	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return schema.InputField{}, schema.WrapError(schema.ErrMissingTypeAnnotation, fmt.Sprintf("parameter '%s' on %s has no type annotation", name, ctx.methodName), nil)
	}

	inputType, fieldType, err := resolveParameterInputTypes(typeNode, source, ctx)
	if err != nil {
		return schema.InputField{}, err
	}

	valNode := node.ChildByFieldName("value")

	if valNode != nil {
		// 1. Direct Input() call
		if isInputCall(valNode, source, ctx.imports) {
			info, err := parseInputCall(valNode, source, name, ctx.scope)
			if err != nil {
				return schema.InputField{}, err
			}
			field, err := inputFieldWithInfo(name, order, inputType, fieldType, info)
			if err != nil {
				return schema.InputField{}, err
			}
			return field, schema.ValidateInputField(field)
		}

		// 2. Reference to Input() via class attribute or static method
		if info, ok := resolveInputReference(valNode, source, ctx.registry); ok {
			field, err := inputFieldWithInfo(name, order, inputType, fieldType, info)
			if err != nil {
				return schema.InputField{}, err
			}
			return field, schema.ValidateInputField(field)
		}

		// 3. Plain default — must be statically resolvable
		if def, ok := resolveDefaultExpr(valNode, source, ctx.scope); ok {
			field := inputField(name, order, inputType, fieldType)
			field.Default = &def
			return field, schema.ValidateInputField(field)
		}

		// Can't resolve — hard error
		valText := Content(valNode, source)
		return schema.InputField{}, schema.WrapError(schema.ErrDefaultNotResolvable,
			fmt.Sprintf("parameter '%s': default `%s` cannot be statically resolved", name, valText), nil)
	}

	// No default — required parameter
	field := inputField(name, order, inputType, fieldType)
	return field, schema.ValidateInputField(field)
}

func isInputCall(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	if node.Type() != "call" {
		return false
	}
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil {
		return false
	}
	name := Content(funcNode, source)
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

	for _, child := range NamedChildren(args) {
		if child.Type() != "keyword_argument" {
			continue
		}
		keyNode := child.ChildByFieldName("name")
		valNode := child.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}

		key := Content(keyNode, source)
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
			if s, ok := resolveStringExpr(valNode, source, scope); ok {
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
		case "accept":
			if s, ok := parseStringLiteral(valNode, source); ok {
				info.Accept = &s
			}
		}
	}

	return info, nil
}
