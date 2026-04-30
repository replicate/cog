package python

import (
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

func parseDefaultValue(node *sitter.Node, source []byte) (schema.DefaultValue, bool) {
	switch node.Type() {
	case "none":
		return schema.DefaultValue{Kind: schema.DefaultNone}, true
	case "true":
		return schema.DefaultValue{Kind: schema.DefaultBool, Bool: true}, true
	case "false":
		return schema.DefaultValue{Kind: schema.DefaultBool, Bool: false}, true
	case "integer":
		text := Content(node, source)
		n, err := strconv.ParseInt(text, 0, 64)
		if err != nil {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultInt, Int: n}, true
	case "float":
		text := Content(node, source)
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultFloat, Float: f}, true
	case "string", "concatenated_string":
		s, ok := parseStringLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultString, Str: s}, true
	case "list":
		items, ok := parseListLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultList, List: items}, true
	case "dictionary":
		keys, vals, ok := parseDictLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultDict, DictKeys: keys, DictVals: vals}, true
	case "set":
		items, ok := parseSetLiteral(node, source)
		if !ok {
			return schema.DefaultValue{}, false
		}
		return schema.DefaultValue{Kind: schema.DefaultSet, List: items}, true
	case "unary_operator":
		text := strings.TrimSpace(Content(node, source))
		if n, err := strconv.ParseInt(text, 0, 64); err == nil {
			return schema.DefaultValue{Kind: schema.DefaultInt, Int: n}, true
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			return schema.DefaultValue{Kind: schema.DefaultFloat, Float: f}, true
		}
		return schema.DefaultValue{}, false
	case "tuple":
		var items []schema.DefaultValue
		for _, child := range NamedChildren(node) {
			if val, ok := parseDefaultValue(child, source); ok {
				items = append(items, val)
			}
		}
		return schema.DefaultValue{Kind: schema.DefaultList, List: items}, true
	}
	return schema.DefaultValue{}, false
}

func parseStringLiteral(node *sitter.Node, source []byte) (string, bool) {
	text := Content(node, source)
	if strings.HasPrefix(text, `"""`) || strings.HasPrefix(text, `'''`) {
		if len(text) >= 6 {
			return text[3 : len(text)-3], true
		}
		return "", false
	}
	if strings.HasPrefix(text, `"`) || strings.HasPrefix(text, `'`) {
		if len(text) >= 2 {
			return text[1 : len(text)-1], true
		}
		return "", false
	}
	if strings.HasPrefix(text, `r"`) || strings.HasPrefix(text, `r'`) {
		if len(text) >= 3 {
			return text[2 : len(text)-1], true
		}
		return "", false
	}
	return "", false
}

func parseNumberLiteral(node *sitter.Node, source []byte) (float64, bool) {
	text := strings.TrimSpace(Content(node, source))
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func parseBoolLiteral(node *sitter.Node, source []byte) (bool, bool) {
	switch node.Type() {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	text := Content(node, source)
	switch text {
	case "True":
		return true, true
	case "False":
		return false, true
	}
	return false, false
}

func parseListLiteral(node *sitter.Node, source []byte) ([]schema.DefaultValue, bool) {
	if node.Type() != "list" {
		return nil, false
	}
	var items []schema.DefaultValue
	for _, child := range NamedChildren(node) {
		val, ok := parseDefaultValue(child, source)
		if !ok {
			return nil, false
		}
		items = append(items, val)
	}
	return items, true
}

func parseDictLiteral(node *sitter.Node, source []byte) ([]schema.DefaultValue, []schema.DefaultValue, bool) {
	if node.Type() != "dictionary" {
		return nil, nil, false
	}
	var keys, vals []schema.DefaultValue
	for _, child := range NamedChildren(node) {
		if child.Type() != "pair" {
			continue
		}
		keyNode := child.ChildByFieldName("key")
		valNode := child.ChildByFieldName("value")
		if keyNode == nil || valNode == nil {
			continue
		}
		k, ok1 := parseDefaultValue(keyNode, source)
		v, ok2 := parseDefaultValue(valNode, source)
		if ok1 && ok2 {
			keys = append(keys, k)
			vals = append(vals, v)
		}
	}
	return keys, vals, true
}

func parseSetLiteral(node *sitter.Node, source []byte) ([]schema.DefaultValue, bool) {
	if node.Type() != "set" {
		return nil, false
	}
	var items []schema.DefaultValue
	for _, child := range NamedChildren(node) {
		val, ok := parseDefaultValue(child, source)
		if !ok {
			return nil, false
		}
		items = append(items, val)
	}
	return items, true
}
