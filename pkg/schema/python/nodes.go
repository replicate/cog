package python

import sitter "github.com/smacker/go-tree-sitter"

// NamedChildren returns all named children of a node.
func NamedChildren(n *sitter.Node) []*sitter.Node {
	count := int(n.NamedChildCount())
	result := make([]*sitter.Node, 0, count)
	for i := range count {
		result = append(result, n.NamedChild(i))
	}
	return result
}

// AllChildren returns all children (named and anonymous) of a node.
func AllChildren(n *sitter.Node) []*sitter.Node {
	count := int(n.ChildCount())
	result := make([]*sitter.Node, 0, count)
	for i := range count {
		result = append(result, n.Child(i))
	}
	return result
}

// Content returns the source text for a node.
func Content(n *sitter.Node, source []byte) string {
	return n.Content(source)
}

func UnwrapClass(node *sitter.Node) *sitter.Node {
	if node.Type() == "class_definition" {
		return node
	}
	if node.Type() == "decorated_definition" {
		for _, c := range NamedChildren(node) {
			if c.Type() == "class_definition" {
				return c
			}
		}
	}
	return nil
}

func UnwrapFunction(node *sitter.Node) *sitter.Node {
	if node.Type() == "function_definition" {
		return node
	}
	if node.Type() == "decorated_definition" {
		for _, c := range NamedChildren(node) {
			if c.Type() == "function_definition" {
				return c
			}
		}
	}
	return nil
}
