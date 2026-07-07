package python

import (
	"fmt"
	"math"
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

func functionIsAsync(node *sitter.Node, _ []byte) bool {
	fn := UnwrapFunction(node)
	if fn == nil {
		return false
	}
	// tree-sitter-python represents an async function as a function_definition
	// with a leading anonymous "async" token, so detecting it via the parse
	// tree is robust to whitespace variations like "async  def".
	for i := 0; i < int(fn.ChildCount()); i++ {
		if fn.Child(i).Type() == "async" {
			return true
		}
	}
	return false
}

func functionConcurrencyMax(node *sitter.Node, source []byte, imports *schema.ImportContext) (*int, error) {
	decorated := decoratedFunctionNode(node)
	if decorated == nil {
		return nil, nil
	}

	for _, child := range NamedChildren(decorated) {
		if child.Type() != "decorator" {
			continue
		}
		concurrencyMax, ok, err := decoratorCogConcurrentMax(child, source, imports)
		if err != nil {
			return nil, err
		}
		if ok {
			return concurrencyMax, nil
		}
	}
	return nil, nil
}

func decoratorCogConcurrentMax(node *sitter.Node, source []byte, imports *schema.ImportContext) (*int, bool, error) {
	expr := decoratorExpression(node)
	if expr == nil || !expressionIsCogConcurrent(expr, source, imports) {
		return nil, false, nil
	}

	call := decoratorCall(node)
	if call == nil {
		concurrencyMax := 1
		return &concurrencyMax, true, nil
	}

	argList := callArgumentList(call)
	if argList == nil || len(NamedChildren(argList)) == 0 {
		concurrencyMax := 1
		return &concurrencyMax, true, nil
	}

	args := NamedChildren(argList)
	if len(args) != 1 || args[0].Type() != "keyword_argument" {
		return nil, true, schema.WrapError(schema.ErrUnsupportedType, "@concurrent only supports literal integer max=... arguments", nil)
	}
	nameNode := args[0].ChildByFieldName("name")
	valueNode := args[0].ChildByFieldName("value")
	if nameNode == nil || valueNode == nil || Content(nameNode, source) != "max" {
		return nil, true, schema.WrapError(schema.ErrUnsupportedType, "@concurrent only supports literal integer max=... arguments", nil)
	}
	val, ok := parseDefaultValue(valueNode, source)
	if !ok || val.Kind != schema.DefaultInt {
		return nil, true, schema.WrapError(schema.ErrUnsupportedType, "@concurrent max must be an integer literal", nil)
	}
	if val.Int < 1 {
		return nil, true, schema.WrapError(schema.ErrUnsupportedType, "@concurrent max must be at least 1", nil)
	}
	maxSupportedInt := int64(math.MaxInt)
	if val.Int > maxSupportedInt {
		return nil, true, schema.WrapError(schema.ErrUnsupportedType, "@concurrent max is too large", nil)
	}
	concurrencyMax := int(val.Int)
	return &concurrencyMax, true, nil
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

func decoratorCall(node *sitter.Node) *sitter.Node {
	children := NamedChildren(node)
	if len(children) == 0 || children[0].Type() != "call" {
		return nil
	}
	return children[0]
}

func callArgumentList(node *sitter.Node) *sitter.Node {
	for _, child := range NamedChildren(node) {
		if child.Type() == "argument_list" {
			return child
		}
	}
	return nil
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
		return attributeIsCogConcurrent(node, source, imports)
	case "identifier":
		return identifierIsCogConcurrent(node, source, imports)
	default:
		return false
	}
}

func attributeIsCogStreaming(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	name, attr, ok := strings.Cut(Content(node, source), ".")
	if !ok || attr != "streaming" {
		return false
	}
	entry, ok := imports.Names.Get(name)
	return ok && entry.Module == "cog" && entry.Original == "cog"
}

func attributeIsCogConcurrent(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	name, attr, ok := strings.Cut(Content(node, source), ".")
	if !ok || attr != "concurrent" {
		return false
	}
	entry, ok := imports.Names.Get(name)
	return ok && entry.Module == "cog" && entry.Original == "cog"
}

func identifierIsCogStreaming(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	entry, ok := imports.Names.Get(Content(node, source))
	return ok && entry.Module == "cog" && entry.Original == "streaming"
}

func identifierIsCogConcurrent(node *sitter.Node, source []byte, imports *schema.ImportContext) bool {
	entry, ok := imports.Names.Get(Content(node, source))
	return ok && entry.Module == "cog" && entry.Original == "concurrent"
}

func supportsStreamingOutput(output schema.SchemaType) bool {
	return output.Kind == schema.SchemaIterator || output.Kind == schema.SchemaConcatIterator
}

func errUnsupported(msg string) error {
	return &schema.SchemaError{Kind: schema.ErrUnsupportedType, Message: fmt.Sprintf("unsupported type: %s", msg)}
}
