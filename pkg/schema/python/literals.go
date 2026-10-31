package python

import (
	"strconv"
	"strings"
	"unicode/utf8"

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

// parseStringLiteral returns the value of a Python string literal as Python evaluates it:
// prefixes and quotes are removed, escape sequences are decoded (except in raw strings),
// and implicitly concatenated literals ("a" "b") are joined. Bytes and f-strings are not
// static str values and are rejected.
func parseStringLiteral(node *sitter.Node, source []byte) (string, bool) {
	switch node.Type() {
	case "concatenated_string":
		var b strings.Builder
		for _, child := range NamedChildren(node) {
			s, ok := parseStringLiteral(child, source)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		return b.String(), true
	case "string":
	default:
		return "", false
	}

	text := Content(node, source)
	prefixLen := 0
	for prefixLen < len(text) && strings.IndexByte("rRuUbBfF", text[prefixLen]) >= 0 {
		prefixLen++
	}
	prefix := strings.ToLower(text[:prefixLen])
	if strings.ContainsAny(prefix, "bf") {
		return "", false
	}
	body := text[prefixLen:]

	var quote string
	switch {
	case strings.HasPrefix(body, `"""`), strings.HasPrefix(body, `'''`):
		quote = body[:3]
	case strings.HasPrefix(body, `"`), strings.HasPrefix(body, `'`):
		quote = body[:1]
	default:
		return "", false
	}
	if len(body) < 2*len(quote) || !strings.HasSuffix(body, quote) {
		return "", false
	}
	body = body[len(quote) : len(body)-len(quote)]

	if strings.Contains(prefix, "r") {
		return body, true
	}
	return unescapePythonString(body), true
}

// pythonHexEscapeWidths maps a hex escape letter to its digit count (\xhh, \uhhhh, \Uhhhhhhhh).
var pythonHexEscapeWidths = map[byte]int{'x': 2, 'u': 4, 'U': 8}

// pythonSimpleEscapes maps single-character escapes to the byte they stand for.
var pythonSimpleEscapes = map[byte]byte{
	'\\': '\\', '\'': '\'', '"': '"',
	'n': '\n', 't': '\t', 'r': '\r',
	'a': '\a', 'b': '\b', 'f': '\f', 'v': '\v',
}

// unescapePythonString decodes the backslash escapes of a non-raw Python string literal
// body. Unrecognized escapes keep their backslash, as they do in Python.
func unescapePythonString(body string) string {
	if !strings.Contains(body, `\`) {
		return body
	}
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' || i+1 >= len(body) {
			b.WriteByte(c)
			continue
		}
		next := body[i+1]
		if decoded, ok := pythonSimpleEscapes[next]; ok {
			b.WriteByte(decoded)
			i++
			continue
		}
		switch {
		case next == '\n':
			// A backslash before a newline continues the literal on the next line.
			i++
		case next == '\r':
			i++
			if i+1 < len(body) && body[i+1] == '\n' {
				i++
			}
		case next >= '0' && next <= '7':
			end := i + 1
			for end < len(body) && end < i+4 && body[end] >= '0' && body[end] <= '7' {
				end++
			}
			n, _ := parseEscapeDigits(body[i+1:end], 8)
			b.WriteRune(n)
			i = end - 1
		case pythonHexEscapeWidths[next] > 0:
			start := i + 2
			end := start + pythonHexEscapeWidths[next]
			if end > len(body) {
				b.WriteByte(c)
				continue
			}
			n, ok := parseEscapeDigits(body[start:end], 16)
			if !ok {
				b.WriteByte(c)
				continue
			}
			b.WriteRune(n)
			i = end - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// parseEscapeDigits parses the octal or hex digits of an escape sequence into a rune.
func parseEscapeDigits(digits string, base rune) (rune, bool) {
	var r rune
	for _, d := range digits {
		var v rune
		switch {
		case d >= '0' && d <= '9':
			v = d - '0'
		case d >= 'a' && d <= 'f':
			v = d - 'a' + 10
		case d >= 'A' && d <= 'F':
			v = d - 'A' + 10
		default:
			return 0, false
		}
		if v >= base {
			return 0, false
		}
		r = r*base + v
		if r > utf8.MaxRune {
			return 0, false
		}
	}
	return r, true
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
		if child.Type() == "pair" {
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
