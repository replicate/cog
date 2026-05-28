package python

import (
	"fmt"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

func parseTypeFromString(s string) (schema.TypeAnnotation, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return schema.TypeAnnotation{}, false
	}

	// Forward reference: quoted string like "MyType" or 'MyType'.
	// Must be checked before union/generic handling so that a quoted
	// union like "TreeNode | None" is first unquoted, then re-parsed.
	if len(s) >= 2 &&
		((strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) ||
			(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'"))) {
		inner := s[1 : len(s)-1]
		return parseTypeFromString(inner)
	}

	// Union: X | Y
	if parts, ok := splitTopLevelPipes(s); ok {
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

	// Simple or qualified identifier.
	for part := range strings.SplitSeq(s, ".") {
		if part == "" {
			return schema.TypeAnnotation{}, false
		}
		for _, c := range part {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
				return schema.TypeAnnotation{}, false
			}
		}
	}
	return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: s}, true
}

func splitTopLevelPipes(s string) ([]string, bool) {
	depth := 0
	start := 0
	parts := []string{}
	hasPipe := false
	for i, c := range s {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
		case '|':
			if depth == 0 {
				hasPipe = true
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if !hasPipe {
		return nil, false
	}
	parts = append(parts, s[start:])
	return parts, true
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

func parseTypeAnnotation(node *sitter.Node, source []byte) (schema.TypeAnnotation, error) {
	// Unwrap `type` wrapper node
	n := node
	if n.Type() == "type" && n.NamedChildCount() > 0 {
		n = n.NamedChild(0)
	}

	switch n.Type() {
	case "identifier":
		return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: Content(n, source)}, nil

	case "subscript":
		value := n.ChildByFieldName("value")
		if value == nil {
			return schema.TypeAnnotation{}, schema.WrapError(schema.ErrParse, "subscript has no value", nil)
		}
		outer := Content(value, source)

		var args []schema.TypeAnnotation
		for _, child := range NamedChildren(n) {
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
		for _, c := range AllChildren(n) {
			if !c.IsNamed() && Content(c, source) == "|" {
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
		return schema.TypeAnnotation{Kind: schema.TypeAnnotSimple, Name: Content(n, source)}, nil

	case "string", "concatenated_string":
		text := Content(n, source)
		inner := strings.TrimLeft(text, "\"'")
		inner = strings.TrimRight(inner, "\"'")
		if ann, ok := parseTypeFromString(inner); ok {
			return ann, nil
		}
		return schema.TypeAnnotation{}, errUnsupported(fmt.Sprintf("string annotation: %s", text))

	default:
		text := Content(n, source)
		if ann, ok := parseTypeFromString(text); ok {
			return ann, nil
		}
		return schema.TypeAnnotation{}, errUnsupported(fmt.Sprintf("%s: %s", n.Type(), text))
	}
}

func functionSupportsStreaming(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	decorated := decoratedFunctionNode(node)
	if decorated == nil {
		return false
	}

	for _, child := range NamedChildren(decorated) {
		if child.Type() == "decorator" && decoratorIsCogStreaming(child, source, imports) {
			return true
		}
	}
	return false
}

func functionConcurrentMax(node *sitter.Node, source []byte, imports *schema.ImportContext) (*int, error) {
	decorated := decoratedFunctionNode(node)
	if decorated == nil {
		return nil, nil
	}

	for _, child := range NamedChildren(decorated) {
		if child.Type() != "decorator" {
			continue
		}
		max, ok, err := decoratorConcurrentMax(child, source, imports)
		if err != nil || ok {
			return max, err
		}
	}
	return nil, nil
}

func decoratedFunctionNode(node *sitter.Node) *sitter.Node {
	if node.Type() == "decorated_definition" {
		return node
	}
	if node.Type() != "function_definition" {
		return nil
	}
	parent := node.Parent()
	if parent == nil || parent.Type() != "decorated_definition" {
		return nil
	}
	return parent
}

func decoratorIsCogStreaming(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	expr := decoratorExpression(node)
	if expr == nil {
		return false
	}
	return expressionIsCogStreaming(expr, source, imports)
}

func decoratorConcurrentMax(node *sitter.Node, source []byte, imports *schema.ImportContext) (*int, bool, error) {
	children := NamedChildren(node)
	if len(children) == 0 {
		return nil, false, nil
	}

	expr := children[0]
	var args *sitter.Node
	if expr.Type() == "call" {
		args = expr.ChildByFieldName("arguments")
		expr = expr.ChildByFieldName("function")
		if expr == nil {
			return nil, false, nil
		}
	}
	if !expressionIsCogConcurrent(expr, source, imports) {
		return nil, false, nil
	}

	max, err := parseConcurrentMaxArguments(args, source)
	if err != nil {
		return nil, true, err
	}
	return &max, true, nil
}

func decoratorExpression(node *sitter.Node) *sitter.Node {
	children := NamedChildren(node)
	if len(children) == 0 {
		return nil
	}
	child := children[0]
	if child.Type() == "call" {
		return child.ChildByFieldName("function")
	}
	return child
}

func expressionIsCogStreaming(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	switch node.Type() {
	case "attribute":
		return attributeIsCogStreaming(node, source, imports)
	case "identifier":
		return identifierIsCogStreaming(node, source, imports)
	default:
		return false
	}
}

func expressionIsCogConcurrent(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	switch node.Type() {
	case "attribute":
		return attributeIsCogDecorator(node, source, imports, "concurrent")
	case "identifier":
		return identifierIsCogDecorator(node, source, imports, "concurrent")
	default:
		return false
	}
}

func attributeIsCogStreaming(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	return attributeIsCogDecorator(node, source, imports, "streaming")
}

func identifierIsCogStreaming(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	return identifierIsCogDecorator(node, source, imports, "streaming")
}

func attributeIsCogDecorator(node *sitter.Node, source []byte, imports *schema.ImportContext, decoratorName string) bool {
	name, attr, ok := strings.Cut(Content(node, source), ".")
	if !ok || attr != decoratorName {
		return false
	}
	entry, ok := imports.Names.Get(name)
	return ok && entry.Module == "cog" && entry.Original == "cog"
}

func identifierIsCogDecorator(node *sitter.Node, source []byte, imports *schema.ImportContext, decoratorName string) bool {
	entry, ok := imports.Names.Get(Content(node, source))
	return ok && entry.Module == "cog" && entry.Original == decoratorName
}

func parseConcurrentMaxArguments(args *sitter.Node, source []byte) (int, error) {
	if args == nil || len(NamedChildren(args)) == 0 {
		return 1, nil
	}

	var max *int
	for _, child := range NamedChildren(args) {
		if child.Type() != "keyword_argument" {
			return 0, concurrentDecoratorError("only the max keyword argument is supported")
		}
		keyNode := child.ChildByFieldName("name")
		valNode := child.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			return 0, concurrentDecoratorError("arguments must be keyword arguments")
		}
		if key := Content(keyNode, source); key != "max" {
			return 0, concurrentDecoratorError(fmt.Sprintf("unknown keyword argument %q", key))
		}
		if max != nil {
			return 0, concurrentDecoratorError("max can only be specified once")
		}
		parsed, err := parsePositiveIntegerLiteral(valNode, source)
		if err != nil {
			return 0, err
		}
		max = &parsed
	}
	if max == nil {
		return 1, nil
	}
	return *max, nil
}

func parsePositiveIntegerLiteral(node *sitter.Node, source []byte) (int, error) {
	if node.Type() != "integer" {
		return 0, concurrentDecoratorError("max must be a positive integer literal")
	}
	value, err := strconv.ParseInt(Content(node, source), 0, 0)
	if err != nil || value <= 0 {
		return 0, concurrentDecoratorError("max must be a positive integer literal")
	}
	return int(value), nil
}

func concurrentDecoratorError(message string) error {
	return schema.WrapError(schema.ErrParse, fmt.Sprintf("@cog.concurrent %s", message), nil)
}

func supportsStreamingOutput(output schema.SchemaType) bool {
	return output.Kind == schema.SchemaIterator || output.Kind == schema.SchemaConcatIterator
}

func errUnsupported(msg string) error {
	return &schema.SchemaError{Kind: schema.ErrUnsupportedType, Message: fmt.Sprintf("unsupported type: %s", msg)}
}
