//! Tree-sitter based Python parser for extracting predictor signatures.
//!
//! Walks the concrete syntax tree to extract:
//! - Import statements (to resolve `cog.Path` vs `pathlib.Path` etc.)
//! - Class definitions (to find BasePredictor subclasses and BaseModel subclasses)
//! - Function definitions (standalone predictor functions)
//! - Parameters with type annotations and default values
//! - `Input()` call keyword arguments

use std::collections::HashMap;

use indexmap::IndexMap;
use tree_sitter::{Node, Parser, Tree};

use crate::error::{Result, SchemaError};
use crate::types::*;

/// Module-level scope: maps variable names to their statically-resolved values.
/// Only populated for top-level assignments whose right-hand side is a literal
/// (list, dict, set, tuple, string, number, etc.).
type ModuleScope = HashMap<String, DefaultValue>;

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/// Parse a Python source file and extract predictor information.
///
/// `predict_ref` is the class or function name (e.g. `"Predictor"` or `"predict"`).
/// `mode` controls whether we look for `predict` or `train` method.
pub fn parse_predictor(source: &str, predict_ref: &str, mode: Mode) -> Result<PredictorInfo> {
    let tree = parse_python(source)?;
    let root = tree.root_node();
    let src = source.as_bytes();

    // 1. Collect imports
    let imports = collect_imports(root, src);

    // 2. Collect module-level variable assignments (for resolving choices=MY_VAR etc.)
    let module_scope = collect_module_scope(root, src);

    // 3. Collect BaseModel subclasses (for structured output types)
    let model_classes = collect_model_classes(root, src, &imports);

    // 4. Collect Input() references from class attributes and static methods
    //    (e.g. `Inputs.prompt`, `Inputs.go_fast_with_default(True)`)
    let input_registry = collect_input_registry(root, src, &imports, &module_scope);

    // 5. Find the target predict/train function
    let method_name = match mode {
        Mode::Predict => "predict",
        Mode::Train => "train",
    };

    let func_node = find_target_function(root, src, predict_ref, method_name)?;

    // 6. Determine if this is a method (has `self` first param) or standalone function
    let params_node = func_node
        .child_by_field_name("parameters")
        .ok_or_else(|| SchemaError::ParseError("function has no parameters node".into()))?;

    let is_method = first_param_is_self(&params_node, src);

    // 7. Extract parameters
    let inputs = extract_inputs(
        &params_node,
        src,
        method_name,
        is_method,
        &imports,
        &input_registry,
        &module_scope,
    )?;

    // 8. Extract return type
    let return_ann = func_node
        .child_by_field_name("return_type")
        .ok_or_else(|| SchemaError::MissingReturnType {
            method: method_name.into(),
        })?;
    let return_type_ann = parse_type_annotation(&return_ann, src)?;
    let output = resolve_output_type(&return_type_ann, &imports, &model_classes)?;

    Ok(PredictorInfo {
        inputs,
        output,
        mode,
    })
}

// ---------------------------------------------------------------------------
// Python parsing
// ---------------------------------------------------------------------------

fn parse_python(source: &str) -> Result<Tree> {
    let mut parser = Parser::new();
    parser
        .set_language(&tree_sitter_python::LANGUAGE.into())
        .map_err(|e| SchemaError::ParseError(format!("failed to set language: {e}")))?;
    parser
        .parse(source, None)
        .ok_or_else(|| SchemaError::ParseError("tree-sitter parse returned None".into()))
}

fn node_text<'a>(node: &Node, src: &'a [u8]) -> &'a str {
    node.utf8_text(src).unwrap_or("")
}

// ---------------------------------------------------------------------------
// Import collection
// ---------------------------------------------------------------------------

fn collect_imports(root: Node, src: &[u8]) -> ImportContext {
    let mut ctx = ImportContext::default();
    let mut cursor = root.walk();

    for child in root.children(&mut cursor) {
        if child.kind() == "import_from_statement" {
            parse_import_from(&child, src, &mut ctx);
        }
    }

    // Always include Python builtins that don't need importing
    for builtin in &["str", "int", "float", "bool", "list", "dict", "set"] {
        ctx.names
            .entry((*builtin).to_string())
            .or_insert_with(|| ("builtins".to_string(), (*builtin).to_string()));
    }
    ctx.names
        .entry("None".to_string())
        .or_insert_with(|| ("builtins".to_string(), "None".to_string()));

    ctx
}

fn parse_import_from(node: &Node, src: &[u8], ctx: &mut ImportContext) {
    let module = match node.child_by_field_name("module_name") {
        Some(n) => node_text(&n, src).to_string(),
        None => return,
    };

    // Walk children looking for imported_name nodes within the import list
    let mut cursor = node.walk();
    for child in node.children(&mut cursor) {
        match child.kind() {
            "dotted_name" => {
                // `from X import name` (single import without parens)
                // Only process if this isn't the module_name itself
                if child.start_byte()
                    != node
                        .child_by_field_name("module_name")
                        .map_or(0, |n| n.start_byte())
                {
                    let name = node_text(&child, src).to_string();
                    ctx.names.insert(name.clone(), (module.clone(), name));
                }
            }
            "import_list" => {
                let mut list_cursor = child.walk();
                for import_child in child.children(&mut list_cursor) {
                    match import_child.kind() {
                        "dotted_name" => {
                            let name = node_text(&import_child, src).to_string();
                            ctx.names.insert(name.clone(), (module.clone(), name));
                        }
                        "aliased_import" => {
                            let orig = import_child
                                .child_by_field_name("name")
                                .map(|n| node_text(&n, src).to_string())
                                .unwrap_or_default();
                            let alias = import_child
                                .child_by_field_name("alias")
                                .map(|n| node_text(&n, src).to_string())
                                .unwrap_or_else(|| orig.clone());
                            ctx.names.insert(alias, (module.clone(), orig));
                        }
                        _ => {}
                    }
                }
            }
            _ => {}
        }
    }
}

// ---------------------------------------------------------------------------
// BaseModel subclass collection
// ---------------------------------------------------------------------------

/// Collect all BaseModel subclasses defined in the file.
/// Returns a map from class name → list of (field_name, type_annotation, default).
fn collect_model_classes(root: Node, src: &[u8], imports: &ImportContext) -> ModelClassMap {
    let mut models = IndexMap::new();
    let mut cursor = root.walk();

    for child in root.children(&mut cursor) {
        let class_node = match child.kind() {
            "class_definition" => child,
            "decorated_definition" => {
                match child
                    .children(&mut child.walk())
                    .find(|c| c.kind() == "class_definition")
                {
                    Some(c) => c,
                    None => continue,
                }
            }
            _ => continue,
        };
        if let Some(name_node) = class_node.child_by_field_name("name") {
            let class_name = node_text(&name_node, src).to_string();

            if !inherits_from_base_model(&class_node, src, imports) {
                continue;
            }

            let fields = extract_class_annotations(&class_node, src);
            models.insert(class_name, fields);
        }
    }

    models
}

fn inherits_from_base_model(class_node: &Node, src: &[u8], imports: &ImportContext) -> bool {
    if let Some(supers) = class_node.child_by_field_name("superclasses") {
        let mut cursor = supers.walk();
        for child in supers.children(&mut cursor) {
            if child.kind() == "identifier" {
                let name = node_text(&child, src);
                if imports.is_base_model(name) || name == "BaseModel" {
                    return true;
                }
            }
        }
    }
    false
}

// NOTE: inherits_from_base_predictor is intentionally not used yet —
// we find the class by name (from cog.yaml predict ref), not by superclass.
// Keeping this for potential future validation.
#[allow(dead_code)]
fn inherits_from_base_predictor(class_node: &Node, src: &[u8], imports: &ImportContext) -> bool {
    if let Some(supers) = class_node.child_by_field_name("superclasses") {
        let mut cursor = supers.walk();
        for child in supers.children(&mut cursor) {
            if child.kind() == "identifier" {
                let name = node_text(&child, src);
                if imports.is_base_predictor(name) || name == "BasePredictor" {
                    return true;
                }
            }
        }
    }
    false
}

// ---------------------------------------------------------------------------
// Input registry — resolves `ClassName.attr` and `ClassName.method(args)`
// references to their underlying Input() calls.
//
// This handles the pattern used in cog-flux where a shared `Inputs` dataclass
// holds reusable Input() definitions:
//
//   @dataclass(frozen=True)
//   class Inputs:
//       prompt = Input(description="Prompt for generated image")
//       @staticmethod
//       def go_fast_with_default(default: bool) -> Input:
//           return Input(description="...", default=default)
//
//   class DevPredictor(Predictor):
//       def predict(self, prompt: str = Inputs.prompt, go_fast: bool = Inputs.go_fast_with_default(True)):
// ---------------------------------------------------------------------------

/// Registry of Input() definitions found as class attributes and static methods.
#[derive(Debug, Default)]
struct InputRegistry {
    /// `"ClassName.attr_name"` → parsed InputCallInfo
    attributes: IndexMap<String, InputCallInfo>,
    /// `"ClassName.method_name"` → byte range of the method's body (for the return Input() call)
    /// We store the raw source range so we can re-parse the Input() call with overridden args.
    method_input_calls: IndexMap<String, InputMethodInfo>,
}

/// Info about a static method that returns an Input() call.
#[derive(Debug)]
struct InputMethodInfo {
    /// Parameter names of the static method (excludes `self`/`cls`)
    param_names: Vec<String>,
    /// The Input() call info extracted from the return statement
    base_info: InputCallInfo,
}

/// Collect all class-level `Input()` attributes and static methods returning `Input()`.
fn collect_input_registry(
    root: Node,
    src: &[u8],
    imports: &ImportContext,
    module_scope: &ModuleScope,
) -> InputRegistry {
    let mut registry = InputRegistry::default();
    let mut cursor = root.walk();

    for child in root.children(&mut cursor) {
        // Unwrap decorated classes: `@dataclass class Inputs: ...`
        let class_node = match child.kind() {
            "class_definition" => child,
            "decorated_definition" => {
                match child
                    .children(&mut child.walk())
                    .find(|c| c.kind() == "class_definition")
                {
                    Some(c) => c,
                    None => continue,
                }
            }
            _ => continue,
        };
        let class_name = match class_node.child_by_field_name("name") {
            Some(n) => node_text(&n, src).to_string(),
            None => continue,
        };
        let body = match class_node.child_by_field_name("body") {
            Some(b) => b,
            None => continue,
        };

        let mut body_cursor = body.walk();
        for stmt in body.children(&mut body_cursor) {
            // Look for bare assignments: `attr = Input(...)`
            let inner = if stmt.kind() == "expression_statement" {
                match stmt.named_child(0) {
                    Some(n) => n,
                    None => continue,
                }
            } else {
                stmt
            };

            if inner.kind() == "assignment" {
                collect_input_attribute(
                    &class_name,
                    &inner,
                    src,
                    imports,
                    module_scope,
                    &mut registry,
                );
            }

            // Look for annotated assignments: `attr: X = Input(...)`
            // (tree-sitter sometimes uses "assignment" for these too)

            // Look for decorated/undecorated function definitions (static methods)
            let func = match inner.kind() {
                "function_definition" => Some(inner),
                "decorated_definition" => inner
                    .children(&mut inner.walk())
                    .find(|c| c.kind() == "function_definition"),
                _ => None,
            };
            if let Some(func) = func {
                collect_input_method(&class_name, &func, src, imports, module_scope, &mut registry);
            }
        }
    }

    registry
}

/// Collect a class attribute that is an Input() call: `attr = Input(...)`
fn collect_input_attribute(
    class_name: &str,
    assignment: &Node,
    src: &[u8],
    imports: &ImportContext,
    module_scope: &ModuleScope,
    registry: &mut InputRegistry,
) {
    // Get the left side (attribute name)
    let left = match assignment.child_by_field_name("left") {
        Some(n) if n.kind() == "identifier" => node_text(&n, src).to_string(),
        _ => return,
    };

    // Get the right side — must be an Input() call
    let right = match assignment.child_by_field_name("right") {
        Some(n) => n,
        None => return,
    };

    if !is_input_call(&right, src, imports) {
        return;
    }

    // Parse the Input() call — use a dummy param name for error reporting
    let key = format!("{class_name}.{left}");
    if let Ok(info) = parse_input_call(&right, src, &key, module_scope) {
        registry.attributes.insert(key, info);
    }
}

/// Collect a static method that returns an Input() call.
fn collect_input_method(
    class_name: &str,
    func: &Node,
    src: &[u8],
    imports: &ImportContext,
    module_scope: &ModuleScope,
    registry: &mut InputRegistry,
) {
    let method_name = match func.child_by_field_name("name") {
        Some(n) => node_text(&n, src).to_string(),
        None => return,
    };

    // Extract parameter names (skip self/cls)
    let params = match func.child_by_field_name("parameters") {
        Some(p) => p,
        None => return,
    };
    let mut param_names = Vec::new();
    let mut params_cursor = params.walk();
    for param in params.children(&mut params_cursor) {
        match param.kind() {
            "identifier" => {
                let name = node_text(&param, src);
                if name != "self" && name != "cls" {
                    param_names.push(name.to_string());
                }
            }
            "typed_parameter" => {
                let mut c = param.walk();
                if let Some(id) = param.children(&mut c).find(|ch| ch.kind() == "identifier") {
                    let name = node_text(&id, src);
                    if name != "self" && name != "cls" {
                        param_names.push(name.to_string());
                    }
                }
            }
            "typed_default_parameter" | "default_parameter" => {
                if let Some(n) = param.child_by_field_name("name") {
                    let name = node_text(&n, src);
                    if name != "self" && name != "cls" {
                        param_names.push(name.to_string());
                    }
                }
            }
            _ => {}
        }
    }

    // Find `return Input(...)` in the method body
    let body = match func.child_by_field_name("body") {
        Some(b) => b,
        None => return,
    };

    if let Some(input_call) = find_return_input_call(&body, src, imports) {
        let key = format!("{class_name}.{method_name}");
        if let Ok(info) = parse_input_call(&input_call, src, &key, module_scope) {
            registry.method_input_calls.insert(
                key,
                InputMethodInfo {
                    param_names,
                    base_info: info,
                },
            );
        }
    }
}

/// Find a `return Input(...)` statement in a function body.
fn find_return_input_call<'a>(
    body: &Node<'a>,
    src: &[u8],
    imports: &ImportContext,
) -> Option<Node<'a>> {
    let mut cursor = body.walk();
    for child in body.children(&mut cursor) {
        if child.kind() == "return_statement" {
            // The return value is the first named child
            if let Some(expr) = child.named_child(0)
                && is_input_call(&expr, src, imports)
            {
                return Some(expr);
            }
        }
    }
    None
}

/// Try to resolve an attribute access (e.g. `Inputs.prompt`) or method call
/// (e.g. `Inputs.go_fast_with_default(True)`) to an InputCallInfo.
fn resolve_input_reference(
    node: &Node,
    src: &[u8],
    registry: &InputRegistry,
) -> Option<InputCallInfo> {
    match node.kind() {
        // `Inputs.prompt` — attribute access
        "attribute" => {
            let text = node_text(node, src);
            registry.attributes.get(text).map(|info| InputCallInfo {
                default: info.default.clone(),
                description: info.description.clone(),
                ge: info.ge,
                le: info.le,
                min_length: info.min_length,
                max_length: info.max_length,
                regex: info.regex.clone(),
                choices: info.choices.clone(),
                deprecated: info.deprecated,
            })
        }
        // `Inputs.go_fast_with_default(True)` — method call
        "call" => {
            let func = node.child_by_field_name("function")?;
            if func.kind() != "attribute" {
                return None;
            }
            let key = node_text(&func, src);
            let method_info = registry.method_input_calls.get(key)?;

            // Start with the base Input() info from the method
            let mut resolved = InputCallInfo {
                default: method_info.base_info.default.clone(),
                description: method_info.base_info.description.clone(),
                ge: method_info.base_info.ge,
                le: method_info.base_info.le,
                min_length: method_info.base_info.min_length,
                max_length: method_info.base_info.max_length,
                regex: method_info.base_info.regex.clone(),
                choices: method_info.base_info.choices.clone(),
                deprecated: method_info.base_info.deprecated,
            };

            // Now override with the call-site positional and keyword arguments.
            // The method's Input() uses parameter names as placeholders for values
            // passed at the call site.
            let args = node.child_by_field_name("arguments")?;

            // Build a map of param_name → call-site value
            let mut arg_values: IndexMap<String, Node> = IndexMap::new();
            let mut positional_idx = 0;
            let mut args_cursor = args.walk();
            for arg in args.children(&mut args_cursor) {
                match arg.kind() {
                    "keyword_argument" => {
                        if let (Some(name_node), Some(val_node)) = (
                            arg.child_by_field_name("name"),
                            arg.child_by_field_name("value"),
                        ) {
                            let name = node_text(&name_node, src).to_string();
                            arg_values.insert(name, val_node);
                        }
                    }
                    _ if arg.is_named() => {
                        // Positional argument
                        if positional_idx < method_info.param_names.len() {
                            let name = method_info.param_names[positional_idx].clone();
                            arg_values.insert(name, arg);
                            positional_idx += 1;
                        }
                    }
                    _ => {}
                }
            }

            // The method's Input() call may use parameter names as values.
            // For example: `return Input(description="...", default=default)`
            // where `default` is a method parameter. We need to resolve these.
            //
            // We handle the common case: if the base_info's default was parsed as None
            // (because `default=default` evaluates the identifier `default` which isn't a literal),
            // but the call-site has a value for the `default` parameter, use that.
            for (param_name, call_site_node) in &arg_values {
                if param_name == "default"
                    && let Some(val) = parse_default_value(call_site_node, src)
                {
                    resolved.default = Some(val);
                }
                if param_name == "description"
                    && let Some(val) = parse_string_literal(call_site_node, src)
                {
                    resolved.description = Some(val);
                }
                if param_name == "ge"
                    && let Some(val) = parse_number_literal(call_site_node, src)
                {
                    resolved.ge = Some(val);
                }
                if param_name == "le"
                    && let Some(val) = parse_number_literal(call_site_node, src)
                {
                    resolved.le = Some(val);
                }
            }

            Some(resolved)
        }
        _ => None,
    }
}

/// Extract annotated assignments from a class body.
/// Handles: `name: type` and `name: type = default`
fn extract_class_annotations(
    class_node: &Node,
    src: &[u8],
) -> Vec<(String, TypeAnnotation, Option<DefaultValue>)> {
    let mut fields = Vec::new();

    let body = match class_node.child_by_field_name("body") {
        Some(b) => b,
        None => return fields,
    };

    let mut cursor = body.walk();
    for child in body.children(&mut cursor) {
        // Look for expression_statement containing type annotations
        let stmt = if child.kind() == "expression_statement" {
            child.child(0)
        } else {
            Some(child)
        };

        if let Some(stmt) = stmt {
            if stmt.kind() == "type" {
                // `name: type` — bare annotation, tree-sitter wraps in `type` node
                // Actually this is an annotated assignment without value
                // tree-sitter Python: `x: int` is expression_statement > type > ...
                // Need to handle this differently
            } else if stmt.kind() == "assignment" {
                // `name: type = value` — annotated assignment with default
                // tree-sitter Python parses `x: int = 5` as an assignment with type annotation
            }

            // The actual pattern in tree-sitter-python for annotated assignments is different.
            // `x: int` → expression_statement > (type (identifier "x") ... )
            // `x: int = 5` → assignment with type annotation
            //
            // Actually, in tree-sitter-python the grammar is:
            // `x: int` → expression_statement containing an `assignment` with no right side?
            // No — it's a standalone type annotation expression.
            //
            // Let me handle both patterns by looking at the raw structure.
        }
    }

    // Simpler approach: walk all children of the body looking for patterns
    fields = extract_annotations_simple(&body, src);

    fields
}

/// Simpler annotation extraction: scan the class body for annotated names.
fn extract_annotations_simple(
    body: &Node,
    src: &[u8],
) -> Vec<(String, TypeAnnotation, Option<DefaultValue>)> {
    let mut fields = Vec::new();
    let body_text = node_text(body, src);
    let _ = body_text; // we use the AST, not text

    let mut cursor = body.walk();
    for child in body.children(&mut cursor) {
        let node = if child.kind() == "expression_statement" {
            // Unwrap expression_statement to get the inner node
            if let Some(inner) = child
                .named_child(0)
                .filter(|_| child.named_child_count() == 1)
            {
                inner
            } else {
                continue;
            }
        } else {
            child
        };

        match node.kind() {
            // `name: type = value` — tree-sitter-python uses "assignment" for annotated assignments too
            "assignment" => {
                if let Some(field) = parse_annotated_assignment(&node, src) {
                    fields.push(field);
                }
            }
            // `name: type` without a value — tree-sitter-python: type annotation expression
            "type" => {
                if let Some(field) = parse_bare_annotation(&node, src) {
                    fields.push(field);
                }
            }
            _ => {}
        }
    }

    fields
}

fn parse_annotated_assignment(
    node: &Node,
    src: &[u8],
) -> Option<(String, TypeAnnotation, Option<DefaultValue>)> {
    // Annotated assignment: `name: type = value`
    // In tree-sitter-python, this has:
    // - left: identifier
    // - type: type annotation
    // - right: value expression
    let left = node.child_by_field_name("left")?;
    let type_node = node.child_by_field_name("type")?;
    let right = node.child_by_field_name("right");

    if left.kind() != "identifier" {
        return None;
    }

    let name = node_text(&left, src).to_string();
    let type_ann = parse_type_annotation(&type_node, src).ok()?;
    let default = right.and_then(|r| parse_default_value(&r, src));

    Some((name, type_ann, default))
}

fn parse_bare_annotation(
    node: &Node,
    src: &[u8],
) -> Option<(String, TypeAnnotation, Option<DefaultValue>)> {
    // Bare type annotation: `name: type`
    // In tree-sitter-python, the expression_statement wraps a type node.
    // The structure might be: type > identifier (the name) : type_annotation
    //
    // Actually, tree-sitter-python represents `x: int` as:
    // (expression_statement (type) )
    // where the `type` node contains the full `x: int` text.
    //
    // This is tricky. Let's use a different approach: look at the node's children.
    // A bare annotation `x: int` in tree-sitter-python is actually parsed as:
    // expression_statement
    //   type
    //     identifier "x"  (but this is actually wrong)
    //
    // The reality is that tree-sitter-python since v0.21+ handles annotations differently.
    // Let me just parse the text directly for this edge case.

    let text = node_text(node, src).trim();

    // For the `type` node wrapping a bare annotation, the children should be
    // the annotated expression. But the exact structure varies by grammar version.
    // Fall back to text parsing for bare annotations in class bodies.
    let parts: Vec<&str> = text.splitn(2, ':').collect();
    if parts.len() != 2 {
        return None;
    }
    let name = parts[0].trim().to_string();
    let type_str = parts[1].trim();

    // Validate name is a valid Python identifier
    if name.is_empty() || !name.chars().next()?.is_alphabetic() && name.chars().next()? != '_' {
        return None;
    }

    // Parse the type string manually (simple cases only for class fields)
    let type_ann = parse_type_from_string(type_str)?;

    Some((name, type_ann, None))
}

/// Parse a type annotation from a string representation.
/// Handles: `str`, `int`, `Optional[str]`, `List[int]`, `str | None`, etc.
fn parse_type_from_string(s: &str) -> Option<TypeAnnotation> {
    let s = s.trim();

    // Check for union syntax: `X | Y`
    if s.contains('|') {
        let members: Vec<TypeAnnotation> = s
            .split('|')
            .filter_map(|part| parse_type_from_string(part.trim()))
            .collect();
        if members.len() >= 2 {
            return Some(TypeAnnotation::Union(members));
        }
        return None;
    }

    // Check for generic syntax: `X[Y]`
    if let Some(bracket_pos) = s.find('[')
        && s.ends_with(']')
    {
        let outer = s[..bracket_pos].trim().to_string();
        let inner_str = &s[bracket_pos + 1..s.len() - 1];
        let inner = parse_type_from_string(inner_str)?;
        return Some(TypeAnnotation::Generic(outer, vec![inner]));
    }

    // Simple identifier
    if s.chars().all(|c| c.is_alphanumeric() || c == '_') {
        return Some(TypeAnnotation::Simple(s.to_string()));
    }

    None
}

// ---------------------------------------------------------------------------
// Target function finding
// ---------------------------------------------------------------------------

/// Find the predict/train function, handling three patterns:
/// 1. Class with method: `class Predictor(BasePredictor): def predict(self, ...)`
/// 2. Non-BasePredictor class: `class Predictor: def predict(self, ...)`
/// 3. Standalone function: `def predict(...)`
fn find_target_function<'a>(
    root: Node<'a>,
    src: &[u8],
    predict_ref: &str,
    method_name: &str,
) -> Result<Node<'a>> {
    let mut cursor = root.walk();

    // First: look for a class with this name
    for child in root.children(&mut cursor) {
        if child.kind() == "class_definition"
            && let Some(name_node) = child.child_by_field_name("name")
            && node_text(&name_node, src) == predict_ref
        {
            // Found the class — now find the method
            return find_method_in_class(child, src, method_name);
        }
    }

    // Second: look for a standalone function with either predict_ref name or method_name
    let mut cursor2 = root.walk();
    for child in root.children(&mut cursor2) {
        if child.kind() == "function_definition" || child.kind() == "decorated_definition" {
            let func = if child.kind() == "decorated_definition" {
                // Unwrap decorator to get the function
                child
                    .children(&mut child.walk())
                    .find(|c| c.kind() == "function_definition")
            } else {
                Some(child)
            };

            if let Some(func) = func
                && let Some(name_node) = func.child_by_field_name("name")
            {
                let name = node_text(&name_node, src);
                if name == predict_ref || name == method_name {
                    return Ok(func);
                }
            }
        }
    }

    Err(SchemaError::PredictorNotFound(predict_ref.to_string()))
}

fn find_method_in_class<'a>(
    class_node: Node<'a>,
    src: &[u8],
    method_name: &str,
) -> Result<Node<'a>> {
    let body = class_node
        .child_by_field_name("body")
        .ok_or_else(|| SchemaError::ParseError("class has no body".into()))?;

    let mut cursor = body.walk();
    for child in body.children(&mut cursor) {
        let func = match child.kind() {
            "function_definition" => Some(child),
            "decorated_definition" => child
                .children(&mut child.walk())
                .find(|c| c.kind() == "function_definition"),
            _ => None,
        };

        if let Some(func) = func
            && let Some(name_node) = func.child_by_field_name("name")
            && node_text(&name_node, src) == method_name
        {
            return Ok(func);
        }
    }

    Err(SchemaError::MethodNotFound(format!(
        "{method_name} not found in class"
    )))
}

// ---------------------------------------------------------------------------
// Parameter extraction
// ---------------------------------------------------------------------------

fn first_param_is_self(params_node: &Node, src: &[u8]) -> bool {
    let mut cursor = params_node.walk();
    for child in params_node.children(&mut cursor) {
        if child.kind() == "identifier" {
            return node_text(&child, src) == "self";
        }
    }
    false
}

fn extract_inputs(
    params_node: &Node,
    src: &[u8],
    method_name: &str,
    skip_self: bool,
    imports: &ImportContext,
    input_registry: &InputRegistry,
    module_scope: &ModuleScope,
) -> Result<IndexMap<String, InputField>> {
    let mut inputs = IndexMap::new();
    let mut order: usize = 0;
    let mut seen_self = false;

    let mut cursor = params_node.walk();
    for child in params_node.children(&mut cursor) {
        match child.kind() {
            // `self` — skip
            "identifier" if !seen_self && skip_self => {
                let name = node_text(&child, src);
                if name == "self" {
                    seen_self = true;
                    continue;
                }
            }

            // `name: type` — typed parameter with no default
            "typed_parameter" => {
                let input = parse_typed_parameter(&child, src, order, method_name, imports)?;
                inputs.insert(input.name.clone(), input);
                order += 1;
            }

            // `name: type = default` — typed parameter with default
            "typed_default_parameter" => {
                let input = parse_typed_default_parameter(
                    &child,
                    src,
                    order,
                    method_name,
                    imports,
                    input_registry,
                    module_scope,
                )?;
                inputs.insert(input.name.clone(), input);
                order += 1;
            }

            // `name = default` — untyped parameter with default (error)
            "default_parameter" => {
                let name_node = child.child_by_field_name("name");
                let param_name = name_node.map(|n| node_text(&n, src)).unwrap_or("<unknown>");
                return Err(SchemaError::MissingTypeAnnotation {
                    method: method_name.into(),
                    param: param_name.into(),
                });
            }

            _ => {}
        }
    }

    Ok(inputs)
}

fn parse_typed_parameter(
    node: &Node,
    src: &[u8],
    order: usize,
    method_name: &str,
    imports: &ImportContext,
) -> Result<InputField> {
    // typed_parameter: name (identifier as first child), type (named field)
    let name = {
        let mut c = node.walk();
        node.children(&mut c)
            .find(|ch| ch.kind() == "identifier")
            .map(|n| node_text(&n, src).to_string())
            .ok_or_else(|| SchemaError::ParseError("typed_parameter has no identifier".into()))?
    };

    let type_node =
        node.child_by_field_name("type")
            .ok_or_else(|| SchemaError::MissingTypeAnnotation {
                method: method_name.into(),
                param: name.clone(),
            })?;

    let type_ann = parse_type_annotation(&type_node, src)?;
    let field_type = resolve_field_type(&type_ann, imports)?;

    Ok(InputField {
        name,
        order,
        field_type,
        default: None,
        description: None,
        ge: None,
        le: None,
        min_length: None,
        max_length: None,
        regex: None,
        choices: None,
        deprecated: None,
    })
}

fn parse_typed_default_parameter(
    node: &Node,
    src: &[u8],
    order: usize,
    method_name: &str,
    imports: &ImportContext,
    input_registry: &InputRegistry,
    module_scope: &ModuleScope,
) -> Result<InputField> {
    let name = node
        .child_by_field_name("name")
        .map(|n| node_text(&n, src).to_string())
        .ok_or_else(|| SchemaError::ParseError("typed_default_parameter has no name".into()))?;

    let type_node =
        node.child_by_field_name("type")
            .ok_or_else(|| SchemaError::MissingTypeAnnotation {
                method: method_name.into(),
                param: name.clone(),
            })?;

    let type_ann = parse_type_annotation(&type_node, src)?;
    let field_type = resolve_field_type(&type_ann, imports)?;

    let value_node = node.child_by_field_name("value");

    if let Some(ref val) = value_node {
        // 1. Direct Input() call: `param: type = Input(...)`
        if is_input_call(val, src, imports) {
            let input_info = parse_input_call(val, src, &name, module_scope)?;
            return Ok(InputField {
                name,
                order,
                field_type,
                default: input_info.default,
                description: input_info.description,
                ge: input_info.ge,
                le: input_info.le,
                min_length: input_info.min_length,
                max_length: input_info.max_length,
                regex: input_info.regex,
                choices: input_info.choices,
                deprecated: input_info.deprecated,
            });
        }

        // 2. Reference to an Input() via class attribute or static method:
        //    `param: type = Inputs.prompt` or `param: type = Inputs.go_fast_with_default(True)`
        if let Some(input_info) = resolve_input_reference(val, src, input_registry) {
            return Ok(InputField {
                name,
                order,
                field_type,
                default: input_info.default,
                description: input_info.description,
                ge: input_info.ge,
                le: input_info.le,
                min_length: input_info.min_length,
                max_length: input_info.max_length,
                regex: input_info.regex,
                choices: input_info.choices,
                deprecated: input_info.deprecated,
            });
        }
    }

    // 3. Plain default value — must be a statically resolvable literal or module-level constant
    if let Some(ref val) = value_node {
        match resolve_default_expr(val, src, module_scope) {
            Some(default) => {
                return Ok(InputField {
                    name,
                    order,
                    field_type,
                    default: Some(default),
                    description: None,
                    ge: None,
                    le: None,
                    min_length: None,
                    max_length: None,
                    regex: None,
                    choices: None,
                    deprecated: None,
                });
            }
            None => {
                // Can't statically resolve this default — error, not silent.
                let val_text = node_text(val, src);
                return Err(SchemaError::Other(format!(
                    "default value for parameter '{name}' cannot be statically resolved: `{val_text}`. \
                     Defaults must be literals (string, int, float, bool, None, list) or Input() calls."
                )));
            }
        }
    }

    // No default at all — required parameter
    Ok(InputField {
        name,
        order,
        field_type,
        default: None,
        description: None,
        ge: None,
        le: None,
        min_length: None,
        max_length: None,
        regex: None,
        choices: None,
        deprecated: None,
    })
}

// ---------------------------------------------------------------------------
// Type annotation parsing
// ---------------------------------------------------------------------------

/// Parse a type annotation AST node into our TypeAnnotation representation.
pub fn parse_type_annotation(node: &Node, src: &[u8]) -> Result<TypeAnnotation> {
    // The `type` field in tree-sitter-python wraps the actual expression.
    // Unwrap if needed.
    let node = if node.kind() == "type" {
        node.named_child(0).unwrap_or(*node)
    } else {
        *node
    };

    match node.kind() {
        "identifier" => {
            let name = node_text(&node, src).to_string();
            Ok(TypeAnnotation::Simple(name))
        }

        "subscript" => {
            // Generic type: `Optional[str]`, `List[int]`, etc.
            let value = node
                .child_by_field_name("value")
                .ok_or_else(|| SchemaError::ParseError("subscript has no value".into()))?;
            let outer = node_text(&value, src).to_string();

            let mut args = Vec::new();
            // Collect all subscript children (the type arguments)
            let mut cursor = node.walk();
            for child in node.children_by_field_name("subscript", &mut cursor) {
                let arg = parse_type_annotation(&child, src)?;
                args.push(arg);
            }

            if args.is_empty() {
                // Bare subscript like `list` without params
                return Ok(TypeAnnotation::Simple(outer));
            }

            Ok(TypeAnnotation::Generic(outer, args))
        }

        "binary_operator" => {
            // Union type: `str | None`
            let left = node
                .child_by_field_name("left")
                .ok_or_else(|| SchemaError::ParseError("binary_operator has no left".into()))?;
            let right = node
                .child_by_field_name("right")
                .ok_or_else(|| SchemaError::ParseError("binary_operator has no right".into()))?;

            // Check that the operator is `|`
            let op_text = node
                .children(&mut node.walk())
                .find(|c| !c.is_named())
                .map(|c| node_text(&c, src))
                .unwrap_or("");
            if op_text != "|" {
                return Err(SchemaError::UnsupportedType(format!(
                    "unsupported binary operator in type annotation: {op_text}"
                )));
            }

            let left_ann = parse_type_annotation(&left, src)?;
            let right_ann = parse_type_annotation(&right, src)?;

            // Flatten nested unions: (A | B) | C → [A, B, C]
            let mut members = Vec::new();
            match left_ann {
                TypeAnnotation::Union(ref inner) => members.extend(inner.clone()),
                _ => members.push(left_ann),
            }
            match right_ann {
                TypeAnnotation::Union(ref inner) => members.extend(inner.clone()),
                _ => members.push(right_ann),
            }

            Ok(TypeAnnotation::Union(members))
        }

        "none" => Ok(TypeAnnotation::Simple("None".into())),

        "attribute" => {
            // `module.Type` — e.g. `cog.Path`
            let text = node_text(&node, src).to_string();
            Ok(TypeAnnotation::Simple(text))
        }

        "string" | "concatenated_string" => {
            // String annotations from `from __future__ import annotations`
            // The string content IS the type annotation — parse it
            let text = node_text(&node, src);
            // Strip quotes
            let inner = text
                .trim_start_matches(['"', '\''])
                .trim_end_matches(['"', '\'']);
            parse_type_from_string(inner)
                .ok_or_else(|| SchemaError::UnsupportedType(format!("string annotation: {text}")))
        }

        other => {
            // Fallback: try to parse the text representation
            let text = node_text(&node, src);
            parse_type_from_string(text)
                .ok_or_else(|| SchemaError::UnsupportedType(format!("{other}: {text}")))
        }
    }
}

// ---------------------------------------------------------------------------
// Input() call parsing
// ---------------------------------------------------------------------------

fn is_input_call(node: &Node, src: &[u8], imports: &ImportContext) -> bool {
    if node.kind() != "call" {
        return false;
    }
    let func = match node.child_by_field_name("function") {
        Some(f) => f,
        None => return false,
    };
    let name = node_text(&func, src);
    name == "Input"
        || (imports
            .names
            .get(name)
            .is_some_and(|(m, n)| m == "cog" && n == "Input"))
}

/// Parsed keyword arguments from an `Input()` call.
#[derive(Debug, Default)]
struct InputCallInfo {
    default: Option<DefaultValue>,
    description: Option<String>,
    ge: Option<f64>,
    le: Option<f64>,
    min_length: Option<u64>,
    max_length: Option<u64>,
    regex: Option<String>,
    choices: Option<Vec<DefaultValue>>,
    deprecated: Option<bool>,
}

fn parse_input_call(
    node: &Node,
    src: &[u8],
    param_name: &str,
    module_scope: &ModuleScope,
) -> Result<InputCallInfo> {
    let mut info = InputCallInfo::default();

    let args = match node.child_by_field_name("arguments") {
        Some(a) => a,
        None => return Ok(info),
    };

    let mut cursor = args.walk();
    for child in args.children(&mut cursor) {
        if child.kind() != "keyword_argument" {
            continue;
        }

        let key_node = match child.child_by_field_name("name") {
            Some(k) => k,
            None => continue,
        };
        let val_node = match child.child_by_field_name("value") {
            Some(v) => v,
            None => continue,
        };

        let key = node_text(&key_node, src);
        match key {
            "default" => {
                info.default = Some(
                    resolve_default_expr(&val_node, src, module_scope)
                        .unwrap_or(DefaultValue::None),
                );
            }
            "default_factory" => {
                return Err(SchemaError::DefaultFactoryNotSupported {
                    param: param_name.into(),
                });
            }
            "description" => {
                info.description = parse_string_literal(&val_node, src);
            }
            "ge" => {
                info.ge = parse_number_literal(&val_node, src);
            }
            "le" => {
                info.le = parse_number_literal(&val_node, src);
            }
            "min_length" => {
                info.min_length = parse_number_literal(&val_node, src).map(|n| n as u64);
            }
            "max_length" => {
                info.max_length = parse_number_literal(&val_node, src).map(|n| n as u64);
            }
            "regex" => {
                info.regex = parse_string_literal(&val_node, src);
            }
            "choices" => {
                // Try literal list first, then resolve against module scope.
                // Hard error if the expression can't be statically resolved.
                info.choices = match parse_list_literal(&val_node, src) {
                    Some(items) => Some(items),
                    None => match resolve_choices_expr(&val_node, src, module_scope) {
                        Some(items) => Some(items),
                        None => {
                            return Err(SchemaError::ChoicesNotResolvable {
                                param: param_name.into(),
                            });
                        }
                    },
                };
            }
            "deprecated" => {
                info.deprecated = parse_bool_literal(&val_node, src);
            }
            _ => {
                // Unknown keyword — ignore (forward-compatible)
            }
        }
    }

    // If Input() is called with no `default` keyword, it means required (default stays None)
    Ok(info)
}

// ---------------------------------------------------------------------------
// Module-level scope collection
// ---------------------------------------------------------------------------

/// Walk top-level statements and collect assignments whose right-hand side is a
/// statically-resolvable literal (list, dict, set, number, string, etc.).
///
/// This enables resolving `choices=MY_LIST` or `choices=list(MY_DICT.keys())`
/// when the referenced variable is defined at module scope with a literal value.
fn collect_module_scope(root: Node, src: &[u8]) -> ModuleScope {
    let mut scope = ModuleScope::new();
    let mut cursor = root.walk();
    for child in root.children(&mut cursor) {
        // `expression_statement` → `assignment`
        let assign = if child.kind() == "expression_statement" {
            match child.named_child(0) {
                Some(n) if n.kind() == "assignment" => n,
                _ => continue,
            }
        } else if child.kind() == "assignment" {
            child
        } else {
            continue;
        };

        // Left side must be a simple identifier (not tuple unpacking, not dotted)
        let left = match assign.child_by_field_name("left") {
            Some(n) if n.kind() == "identifier" => node_text(&n, src).to_string(),
            _ => continue,
        };

        // Right side must be a statically-parseable literal
        if let Some(right) = assign.child_by_field_name("right") {
            if let Some(val) = parse_default_value(&right, src) {
                scope.insert(left, val);
            }
        }
    }
    scope
}

/// Try to resolve any expression to a `DefaultValue` by first attempting literal
/// parsing, then falling back to module scope lookup for identifiers.
///
/// This handles `default=MY_CONSTANT` where `MY_CONSTANT = "value"` is defined
/// at module scope, as well as plain literals.
fn resolve_default_expr(
    node: &Node,
    src: &[u8],
    scope: &ModuleScope,
) -> Option<DefaultValue> {
    // 1. Try literal parsing first (covers all literal node kinds)
    if let Some(val) = parse_default_value(node, src) {
        return Some(val);
    }

    // 2. Identifier referencing a module-level variable
    if node.kind() == "identifier" {
        let name = node_text(node, src);
        return scope.get(name).cloned();
    }

    None
}

/// Try to statically resolve a `choices=` expression against the module scope.
///
/// Handles these patterns:
///   - Literal list:          `choices=["a", "b"]`       → already works via parse_list_literal
///   - Identifier:            `choices=MY_LIST`           → look up in scope
///   - Dict keys call:        `choices=list(D.keys())`    → look up D, extract keys
///   - Dict values call:      `choices=list(D.values())`  → look up D, extract values
///   - Concatenation:         `choices=expr + expr`        → resolve both sides, concatenate
///
/// Returns `None` if the expression cannot be resolved.
fn resolve_choices_expr(
    node: &Node,
    src: &[u8],
    scope: &ModuleScope,
) -> Option<Vec<DefaultValue>> {
    match node.kind() {
        // 1. Literal list — delegate to existing parser
        "list" => parse_list_literal(node, src),

        // 2. Identifier referencing a module-level variable
        "identifier" => {
            let name = node_text(node, src);
            match scope.get(name)? {
                DefaultValue::List(items) => Some(items.clone()),
                _ => None, // e.g. referencing a dict or string directly isn't a valid choices list
            }
        }

        // 3. Call expression: `list(DICT.keys())` or `list(DICT.values())`
        "call" => resolve_choices_call(node, src, scope),

        // 4. Binary `+` concatenation: `list(D.keys()) + ["custom"]`
        "binary_operator" => {
            let op_node = node
                .children(&mut node.walk())
                .find(|c| c.kind() == "+" || (c.kind() == "binary_operator" && false))?;
            if op_node.kind() != "+" {
                return None;
            }
            // tree-sitter binary_operator has fields: left, right, operator
            // but the operator is an anonymous "+" child. The named children are left and right.
            let left = node.child_by_field_name("left")?;
            let right = node.child_by_field_name("right")?;
            let mut result = resolve_choices_expr(&left, src, scope)?;
            result.extend(resolve_choices_expr(&right, src, scope)?);
            Some(result)
        }

        _ => None,
    }
}

/// Resolve `list(X.keys())` or `list(X.values())` against module scope.
fn resolve_choices_call(
    node: &Node,
    src: &[u8],
    scope: &ModuleScope,
) -> Option<Vec<DefaultValue>> {
    // The call must be `list(...)`
    let func = node.child_by_field_name("function")?;
    if node_text(&func, src) != "list" {
        return None;
    }

    let args = node.child_by_field_name("arguments")?;
    // Should have exactly one positional argument
    let arg = {
        let mut cursor = args.walk();
        args.children(&mut cursor)
            .find(|c| c.is_named() && c.kind() != "(" && c.kind() != ")")?
    };

    // The argument should be a method call: `X.keys()` or `X.values()`
    if arg.kind() != "call" {
        return None;
    }
    let inner_func = arg.child_by_field_name("function")?;
    if inner_func.kind() != "attribute" {
        return None;
    }

    // attribute node has: object, attribute
    let obj = inner_func.child_by_field_name("object")?;
    let attr = inner_func.child_by_field_name("attribute")?;

    if obj.kind() != "identifier" {
        return None;
    }
    let var_name = node_text(&obj, src);
    let method_name = node_text(&attr, src);

    let dict_val = scope.get(var_name)?;
    match (dict_val, method_name) {
        (DefaultValue::Dict(pairs), "keys") => {
            Some(pairs.iter().map(|(k, _)| k.clone()).collect())
        }
        (DefaultValue::Dict(pairs), "values") => {
            Some(pairs.iter().map(|(_, v)| v.clone()).collect())
        }
        _ => None,
    }
}

// ---------------------------------------------------------------------------
// Default value / literal parsing
// ---------------------------------------------------------------------------

fn parse_default_value(node: &Node, src: &[u8]) -> Option<DefaultValue> {
    match node.kind() {
        "none" => Some(DefaultValue::None),
        "true" => Some(DefaultValue::Bool(true)),
        "false" => Some(DefaultValue::Bool(false)),
        "integer" => {
            let text = node_text(node, src);
            text.parse::<i64>().ok().map(DefaultValue::Integer)
        }
        "float" => {
            let text = node_text(node, src);
            text.parse::<f64>().ok().map(DefaultValue::Float)
        }
        "string" | "concatenated_string" => {
            parse_string_literal(node, src).map(DefaultValue::String)
        }
        "list" => {
            let items = parse_list_literal(node, src)?;
            Some(DefaultValue::List(items))
        }
        "dictionary" => {
            let pairs = parse_dict_literal(node, src)?;
            Some(DefaultValue::Dict(pairs))
        }
        "set" => {
            let items = parse_set_literal(node, src)?;
            Some(DefaultValue::Set(items))
        }
        "unary_operator" => {
            // Handle negative numbers: `-1`, `-3.14`
            let text = node_text(node, src).trim().to_string();
            if let Ok(n) = text.parse::<i64>() {
                Some(DefaultValue::Integer(n))
            } else if let Ok(f) = text.parse::<f64>() {
                Some(DefaultValue::Float(f))
            } else {
                None
            }
        }
        "tuple" => {
            // Treat tuples as lists for JSON purposes
            let mut items = Vec::new();
            let mut cursor = node.walk();
            for child in node.children(&mut cursor) {
                if child.is_named()
                    && let Some(val) = parse_default_value(&child, src)
                {
                    items.push(val);
                }
            }
            Some(DefaultValue::List(items))
        }
        _ => None,
    }
}

fn parse_string_literal(node: &Node, src: &[u8]) -> Option<String> {
    let text = node_text(node, src);
    // Strip various quote styles: "...", '...', """...""", '''...'''
    let inner = if text.starts_with("\"\"\"") || text.starts_with("'''") {
        &text[3..text.len() - 3]
    } else if text.starts_with('"') || text.starts_with('\'') {
        &text[1..text.len() - 1]
    } else if text.starts_with("r\"") || text.starts_with("r'") {
        &text[2..text.len() - 1]
    } else {
        return None;
    };
    Some(inner.to_string())
}

fn parse_number_literal(node: &Node, src: &[u8]) -> Option<f64> {
    let text = node_text(node, src).trim();
    text.parse::<f64>().ok()
}

fn parse_bool_literal(node: &Node, src: &[u8]) -> Option<bool> {
    match node.kind() {
        "true" => Some(true),
        "false" => Some(false),
        _ => {
            let text = node_text(node, src);
            match text {
                "True" => Some(true),
                "False" => Some(false),
                _ => None,
            }
        }
    }
}

fn parse_list_literal(node: &Node, src: &[u8]) -> Option<Vec<DefaultValue>> {
    if node.kind() != "list" {
        return None;
    }
    let mut items = Vec::new();
    let mut cursor = node.walk();
    for child in node.children(&mut cursor) {
        if child.is_named()
            && let Some(val) = parse_default_value(&child, src)
        {
            items.push(val);
        }
    }
    Some(items)
}

fn parse_dict_literal(node: &Node, src: &[u8]) -> Option<Vec<(DefaultValue, DefaultValue)>> {
    if node.kind() != "dictionary" {
        return None;
    }
    let mut pairs = Vec::new();
    let mut cursor = node.walk();
    for child in node.children(&mut cursor) {
        if child.kind() == "pair" {
            let key = child
                .child_by_field_name("key")
                .and_then(|k| parse_default_value(&k, src));
            let value = child
                .child_by_field_name("value")
                .and_then(|v| parse_default_value(&v, src));
            if let (Some(k), Some(v)) = (key, value) {
                pairs.push((k, v));
            }
        }
    }
    Some(pairs)
}

fn parse_set_literal(node: &Node, src: &[u8]) -> Option<Vec<DefaultValue>> {
    if node.kind() != "set" {
        return None;
    }
    let mut items = Vec::new();
    let mut cursor = node.walk();
    for child in node.children(&mut cursor) {
        if child.is_named()
            && let Some(val) = parse_default_value(&child, src)
        {
            items.push(val);
        }
    }
    Some(items)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(source: &str, predict_ref: &str) -> PredictorInfo {
        parse_predictor(source, predict_ref, Mode::Predict).unwrap()
    }

    #[test]
    fn test_simple_string_predictor() {
        let source = r#"
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, s: str) -> str:
        return "hello " + s
"#;
        let info = parse(source, "Predictor");
        assert_eq!(info.inputs.len(), 1);
        let s = &info.inputs["s"];
        assert_eq!(s.field_type.primitive, PrimitiveType::String);
        assert_eq!(s.field_type.repetition, Repetition::Required);
        assert!(s.default.is_none());
        assert_eq!(info.output.kind, OutputKind::Single);
        assert_eq!(info.output.primitive, Some(PrimitiveType::String));
    }

    #[test]
    fn test_multiple_inputs_with_defaults() {
        let source = r#"
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(
        self,
        image: Path = Input(description="Grayscale input image"),
        scale: float = Input(description="Factor to scale image by", ge=0, le=10, default=1.5),
    ) -> Path:
        pass
"#;
        let info = parse(source, "Predictor");
        assert_eq!(info.inputs.len(), 2);

        let image = &info.inputs["image"];
        assert_eq!(image.field_type.primitive, PrimitiveType::Path);
        assert!(image.default.is_none()); // no default in Input()
        assert_eq!(image.description.as_deref(), Some("Grayscale input image"));
        assert!(image.is_required());

        let scale = &info.inputs["scale"];
        assert_eq!(scale.field_type.primitive, PrimitiveType::Float);
        assert_eq!(scale.default, Some(DefaultValue::Float(1.5)));
        assert_eq!(scale.ge, Some(0.0));
        assert_eq!(scale.le, Some(10.0));
        assert!(!scale.is_required());
    }

    #[test]
    fn test_optional_input() {
        let source = r#"
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(
        self,
        test_image: Path | None = Input(description="Test image", default=None),
    ) -> Path:
        pass
"#;
        let info = parse(source, "Predictor");
        let img = &info.inputs["test_image"];
        assert_eq!(img.field_type.repetition, Repetition::Optional);
        assert_eq!(img.field_type.primitive, PrimitiveType::Path);
        assert_eq!(img.default, Some(DefaultValue::None));
    }

    #[test]
    fn test_list_input() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, paths: list[str] = Input(description="Paths")) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let paths = &info.inputs["paths"];
        assert_eq!(paths.field_type.repetition, Repetition::Repeated);
        assert_eq!(paths.field_type.primitive, PrimitiveType::String);
    }

    #[test]
    fn test_choices() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, color: str = Input(choices=["red", "green", "blue"])) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let color = &info.inputs["color"];
        assert!(color.choices.is_some());
        let choices = color.choices.as_ref().unwrap();
        assert_eq!(choices.len(), 3);
    }

    #[test]
    fn test_standalone_function() {
        let source = r#"
from cog import Input

def predict(text: str = Input(default="world")) -> str:
    return f"hello {text}"
"#;
        let info = parse(source, "predict");
        assert_eq!(info.inputs.len(), 1);
        let text = &info.inputs["text"];
        assert_eq!(text.default, Some(DefaultValue::String("world".into())));
    }

    #[test]
    fn test_iterator_output() {
        let source = r#"
from typing import Iterator
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, count: int) -> Iterator[str]:
        for i in range(count):
            yield f"chunk {i}"
"#;
        let info = parse(source, "Predictor");
        assert_eq!(info.output.kind, OutputKind::Iterator);
        assert_eq!(info.output.primitive, Some(PrimitiveType::String));
    }

    #[test]
    fn test_default_factory_error() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, items: list[str] = Input(default_factory=list)) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        assert!(matches!(
            err,
            SchemaError::DefaultFactoryNotSupported { .. }
        ));
    }

    #[test]
    fn test_train_mode() {
        let source = r#"
from cog import Input, Path

def train(n: int) -> Path:
    pass
"#;
        let info = parse_predictor(source, "train", Mode::Train).unwrap();
        assert_eq!(info.mode, Mode::Train);
        assert_eq!(info.inputs.len(), 1);
    }

    #[test]
    fn test_non_base_predictor_class() {
        let source = r#"
from cog import Input

class Predictor:
    def predict(self, text: str = Input(default="hello")) -> str:
        return f"hello {text}"
"#;
        let info = parse(source, "Predictor");
        assert_eq!(info.inputs.len(), 1);
        assert_eq!(
            info.inputs["text"].default,
            Some(DefaultValue::String("hello".into()))
        );
    }

    // -----------------------------------------------------------------------
    // choices= resolution tests
    // -----------------------------------------------------------------------

    /// Literal list in choices= — the baseline that always worked.
    #[test]
    fn test_choices_literal_list() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=["a", "b", "c"])) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["x"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 3);
        assert_eq!(choices[0], DefaultValue::String("a".into()));
        assert_eq!(choices[1], DefaultValue::String("b".into()));
        assert_eq!(choices[2], DefaultValue::String("c".into()));
    }

    /// choices= referencing a module-level list variable.
    #[test]
    fn test_choices_module_level_list_var() {
        let source = r#"
from cog import BasePredictor, Input

MY_CHOICES = ["x", "y", "z"]

class Predictor(BasePredictor):
    def predict(self, v: str = Input(choices=MY_CHOICES)) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["v"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 3);
        assert_eq!(choices[0], DefaultValue::String("x".into()));
        assert_eq!(choices[1], DefaultValue::String("y".into()));
        assert_eq!(choices[2], DefaultValue::String("z".into()));
    }

    /// choices=list(DICT.keys()) �� the cog-flux pattern.
    #[test]
    fn test_choices_list_dict_keys() {
        let source = r#"
from cog import BasePredictor, Input

ASPECT_RATIOS = {
    "1:1": (1024, 1024),
    "16:9": (1344, 768),
    "2:3": (832, 1216),
}

class Predictor(BasePredictor):
    def predict(self, ar: str = Input(choices=list(ASPECT_RATIOS.keys()), default="1:1")) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["ar"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 3);
        assert_eq!(choices[0], DefaultValue::String("1:1".into()));
        assert_eq!(choices[1], DefaultValue::String("16:9".into()));
        assert_eq!(choices[2], DefaultValue::String("2:3".into()));
    }

    /// choices=list(DICT.values()) — extract values instead of keys.
    #[test]
    fn test_choices_list_dict_values() {
        let source = r#"
from cog import BasePredictor, Input

LABELS = {"fast": "Fast Mode", "slow": "Slow Mode"}

class Predictor(BasePredictor):
    def predict(self, m: str = Input(choices=list(LABELS.values()))) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["m"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 2);
        assert_eq!(choices[0], DefaultValue::String("Fast Mode".into()));
        assert_eq!(choices[1], DefaultValue::String("Slow Mode".into()));
    }

    /// choices=list(DICT.keys()) + ["custom"] — concatenation with a literal.
    #[test]
    fn test_choices_dict_keys_plus_literal() {
        let source = r#"
from cog import BasePredictor, Input

SIZES = {"small": 256, "large": 1024}

class Predictor(BasePredictor):
    def predict(self, s: str = Input(choices=list(SIZES.keys()) + ["custom"])) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["s"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 3);
        assert_eq!(choices[0], DefaultValue::String("small".into()));
        assert_eq!(choices[1], DefaultValue::String("large".into()));
        assert_eq!(choices[2], DefaultValue::String("custom".into()));
    }

    /// choices= with integer values from a dict.
    #[test]
    fn test_choices_integer_dict_keys() {
        let source = r#"
from cog import BasePredictor, Input

STEP_LABELS = {1: "one", 2: "two", 4: "four"}

class Predictor(BasePredictor):
    def predict(self, steps: int = Input(choices=list(STEP_LABELS.keys()), default=1)) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["steps"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 3);
        assert_eq!(choices[0], DefaultValue::Integer(1));
        assert_eq!(choices[1], DefaultValue::Integer(2));
        assert_eq!(choices[2], DefaultValue::Integer(4));
    }

    /// choices= referencing a variable that is NOT a list → hard error.
    #[test]
    fn test_choices_var_not_a_list_errors() {
        let source = r#"
from cog import BasePredictor, Input

NOT_A_LIST = "oops"

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=NOT_A_LIST)) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        assert!(
            matches!(err, SchemaError::ChoicesNotResolvable { .. }),
            "expected ChoicesNotResolvable, got: {err}"
        );
    }

    /// choices= referencing a variable that doesn't exist → hard error.
    #[test]
    fn test_choices_undefined_var_errors() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=DOES_NOT_EXIST)) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        assert!(
            matches!(err, SchemaError::ChoicesNotResolvable { .. }),
            "expected ChoicesNotResolvable, got: {err}"
        );
    }

    /// choices= with a function call that isn't list(X.keys()) → hard error.
    #[test]
    fn test_choices_arbitrary_call_errors() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=get_choices())) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        assert!(
            matches!(err, SchemaError::ChoicesNotResolvable { .. }),
            "expected ChoicesNotResolvable, got: {err}"
        );
    }

    /// choices= with a list comprehension → hard error.
    #[test]
    fn test_choices_list_comprehension_errors() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=[f"{i}x" for i in range(5)])) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        assert!(
            matches!(err, SchemaError::ChoicesNotResolvable { .. }),
            "expected ChoicesNotResolvable, got: {err}"
        );
    }

    /// choices= via InputRegistry attribute (class-level Input() with dict keys).
    #[test]
    fn test_choices_input_registry_dict_keys() {
        let source = r#"
from dataclasses import dataclass
from cog import BasePredictor, Input

RATIOS = {"1:1": (1024, 1024), "16:9": (1344, 768)}

@dataclass(frozen=True)
class Inputs:
    ar = Input(description="Aspect ratio", choices=list(RATIOS.keys()), default="1:1")

class Predictor(BasePredictor):
    def predict(self, ar: str = Inputs.ar) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["ar"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 2);
        assert_eq!(choices[0], DefaultValue::String("1:1".into()));
        assert_eq!(choices[1], DefaultValue::String("16:9".into()));
    }

    /// choices= with concatenation of two module-level lists.
    #[test]
    fn test_choices_concat_two_vars() {
        let source = r#"
from cog import BasePredictor, Input

BASE = ["a", "b"]
EXTRA = ["c"]

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=BASE + EXTRA)) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        let choices = info.inputs["x"].choices.as_ref().unwrap();
        assert_eq!(choices.len(), 3);
        assert_eq!(choices[0], DefaultValue::String("a".into()));
        assert_eq!(choices[1], DefaultValue::String("b".into()));
        assert_eq!(choices[2], DefaultValue::String("c".into()));
    }

    /// Verify that the error message includes the parameter name.
    #[test]
    fn test_choices_error_includes_param_name() {
        let source = r#"
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self, my_param: str = Input(choices=some_func())) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        let msg = err.to_string();
        assert!(
            msg.contains("my_param"),
            "error should mention param name, got: {msg}"
        );
    }

    /// Module scope only collects top-level assignments, not nested ones.
    #[test]
    fn test_choices_nested_var_not_in_scope() {
        let source = r#"
from cog import BasePredictor, Input

def helper():
    NESTED = ["a", "b"]

class Predictor(BasePredictor):
    def predict(self, x: str = Input(choices=NESTED)) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        assert!(matches!(err, SchemaError::ChoicesNotResolvable { .. }));
    }

    // -----------------------------------------------------------------------
    // default= resolution tests
    // -----------------------------------------------------------------------

    /// default= referencing a module-level string constant inside Input().
    #[test]
    fn test_default_module_level_string_in_input() {
        let source = r#"
from cog import BasePredictor, Input

DEFAULT_RATIO = "1:1"

class Predictor(BasePredictor):
    def predict(self, ar: str = Input(default=DEFAULT_RATIO)) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        assert_eq!(
            info.inputs["ar"].default,
            Some(DefaultValue::String("1:1".into()))
        );
    }

    /// default= referencing a module-level integer constant inside Input().
    #[test]
    fn test_default_module_level_int_in_input() {
        let source = r#"
from cog import BasePredictor, Input

DEFAULT_STEPS = 50

class Predictor(BasePredictor):
    def predict(self, steps: int = Input(default=DEFAULT_STEPS)) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        assert_eq!(
            info.inputs["steps"].default,
            Some(DefaultValue::Integer(50))
        );
    }

    /// default= referencing a module-level list constant inside Input().
    #[test]
    fn test_default_module_level_list_in_input() {
        let source = r#"
from cog import BasePredictor, Input

DEFAULT_TAGS = ["a", "b"]

class Predictor(BasePredictor):
    def predict(self, tags: list[str] = Input(default=DEFAULT_TAGS)) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        assert_eq!(
            info.inputs["tags"].default,
            Some(DefaultValue::List(vec![
                DefaultValue::String("a".into()),
                DefaultValue::String("b".into()),
            ]))
        );
    }

    /// Plain default (no Input()) referencing a module-level constant.
    #[test]
    fn test_default_module_level_var_plain() {
        let source = r#"
from cog import BasePredictor

MY_DEFAULT = "hello"

class Predictor(BasePredictor):
    def predict(self, text: str = MY_DEFAULT) -> str:
        pass
"#;
        let info = parse(source, "Predictor");
        assert_eq!(
            info.inputs["text"].default,
            Some(DefaultValue::String("hello".into()))
        );
    }

    /// Plain default referencing an undefined variable → hard error.
    #[test]
    fn test_default_undefined_var_plain_errors() {
        let source = r#"
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, text: str = UNDEFINED_VAR) -> str:
        pass
"#;
        let err = parse_predictor(source, "Predictor", Mode::Predict).unwrap_err();
        let msg = err.to_string();
        assert!(
            msg.contains("cannot be statically resolved"),
            "expected resolution error, got: {msg}"
        );
    }
}
