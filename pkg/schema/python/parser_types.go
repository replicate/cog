package python

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

func ptr[T any](v T) *T {
	return &v
}

func parseTypeAnnotation(node *sitter.Node, source []byte) (schema.TypeAnnotation, error) {
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

func errUnsupported(msg string) error {
	return &schema.SchemaError{Kind: schema.ErrUnsupportedType, Message: fmt.Sprintf("unsupported type: %s", msg)}
}

func parseTypeFromString(s string) (schema.TypeAnnotation, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return schema.TypeAnnotation{}, false
	}

	if len(s) >= 2 && ((strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) || (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'"))) {
		inner := s[1 : len(s)-1]
		return parseTypeFromString(inner)
	}

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

	bracketPos := strings.Index(s, "[")
	if bracketPos >= 0 && strings.HasSuffix(s, "]") {
		outer := strings.TrimSpace(s[:bracketPos])
		innerStr := s[bracketPos+1 : len(s)-1]
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

	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return schema.TypeAnnotation{}, false
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
