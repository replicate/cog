package python

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/replicate/cog/pkg/schema"
)

func CollectImports(root *sitter.Node, source []byte) *schema.ImportContext {
	ctx := schema.NewImportContext()

	for _, child := range NamedChildren(root) {
		switch child.Type() {
		case "import_from_statement":
			parseImportFrom(child, source, ctx)
		case "import_statement":
			parseImport(child, source, ctx)
		}
	}

	for _, builtin := range []string{"str", "int", "float", "bool", "list", "dict", "set"} {
		if _, ok := ctx.Names.Get(builtin); !ok {
			ctx.Names.Set(builtin, schema.ImportEntry{Module: "builtins", Original: builtin})
		}
	}
	if _, ok := ctx.Names.Get("None"); !ok {
		ctx.Names.Set("None", schema.ImportEntry{Module: "builtins", Original: "None"})
	}

	return ctx
}

func setImport(ctx *schema.ImportContext, localName, module, original string) {
	ctx.Names.Set(localName, schema.ImportEntry{Module: module, Original: original})
}

func aliasedImportParts(node *sitter.Node, source []byte) (string, string, bool) {
	origNode := node.ChildByFieldName("name")
	if origNode == nil {
		return "", "", false
	}
	original := Content(origNode, source)
	alias := original
	if aliasNode := node.ChildByFieldName("alias"); aliasNode != nil {
		alias = Content(aliasNode, source)
	}
	return original, alias, true
}

func addImportFromNode(ctx *schema.ImportContext, module string, node *sitter.Node, source []byte) {
	switch node.Type() {
	case "dotted_name":
		name := Content(node, source)
		setImport(ctx, name, module, name)
	case "aliased_import":
		if original, alias, ok := aliasedImportParts(node, source); ok {
			setImport(ctx, alias, module, original)
		}
	}
}

func parseImportFrom(node *sitter.Node, source []byte, ctx *schema.ImportContext) {
	moduleNode := node.ChildByFieldName("module_name")
	if moduleNode == nil {
		return
	}
	module := Content(moduleNode, source)

	for _, child := range AllChildren(node) {
		switch child.Type() {
		case "dotted_name":
			if child.StartByte() != moduleNode.StartByte() {
				addImportFromNode(ctx, module, child, source)
			}
		case "aliased_import":
			addImportFromNode(ctx, module, child, source)
		case "import_list":
			for _, importChild := range AllChildren(child) {
				addImportFromNode(ctx, module, importChild, source)
			}
		}
	}
}

func parseImport(node *sitter.Node, source []byte, ctx *schema.ImportContext) {
	text := strings.TrimSpace(Content(node, source))
	imports := strings.TrimSpace(strings.TrimPrefix(text, "import "))
	for part := range strings.SplitSeq(imports, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		module := part
		alias := part
		if before, after, ok := strings.Cut(part, " as "); ok {
			module = strings.TrimSpace(before)
			alias = strings.TrimSpace(after)
		}
		setImport(ctx, alias, module, module)
	}
}
