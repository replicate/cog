//! Type system for Cog predictor schemas.
//!
//! Mirrors the Python ADT in `python/cog/_adt.py`. Maps Python type annotations
//! to an intermediate representation, then to JSON Schema / OpenAPI types.

use indexmap::IndexMap;
use serde_json::{Map, Value, json};

use crate::error::{Result, SchemaError};

/// Map of BaseModel class names to their fields: (field_name, type_annotation, default_value).
pub type ModelClassMap = IndexMap<String, Vec<(String, TypeAnnotation, Option<DefaultValue>)>>;

// ---------------------------------------------------------------------------
// Primitive types
// ---------------------------------------------------------------------------

/// Mirrors `PrimitiveType` in `_adt.py`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum PrimitiveType {
    Bool,
    Float,
    Integer,
    String,
    /// `cog.Path` — serialised as `{"type":"string","format":"uri"}`
    Path,
    /// `cog.File` (deprecated) — same wire format as Path
    File,
    /// `cog.Secret` — write-only, masked
    Secret,
    /// `typing.Any` or unresolved — opaque object
    Any,
}

impl PrimitiveType {
    /// JSON Schema fragment for this primitive.
    pub fn json_type(self) -> Value {
        match self {
            Self::Bool => json!({"type": "boolean"}),
            Self::Float => json!({"type": "number"}),
            Self::Integer => json!({"type": "integer"}),
            Self::String => json!({"type": "string"}),
            Self::Path => json!({"type": "string", "format": "uri"}),
            Self::File => json!({"type": "string", "format": "uri"}),
            Self::Secret => json!({
                "type": "string",
                "format": "password",
                "writeOnly": true,
                "x-cog-secret": true
            }),
            Self::Any => json!({"type": "object"}),
        }
    }

    /// Resolve a simple type name (already import-resolved) to a primitive.
    /// Returns `None` if the name isn't a known primitive.
    pub fn from_name(name: &str) -> Option<Self> {
        match name {
            "bool" => Some(Self::Bool),
            "float" => Some(Self::Float),
            "int" => Some(Self::Integer),
            "str" => Some(Self::String),
            "Path" => Some(Self::Path),
            "File" => Some(Self::File),
            "Secret" => Some(Self::Secret),
            "Any" => Some(Self::Any),
            _ => None,
        }
    }
}

// ---------------------------------------------------------------------------
// Repetition / cardinality
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Repetition {
    /// Bare type — `str`
    Required,
    /// `Optional[str]` or `str | None`
    Optional,
    /// `list[str]` or `List[str]`
    Repeated,
}

// ---------------------------------------------------------------------------
// Field type  (primitive + repetition)
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FieldType {
    pub primitive: PrimitiveType,
    pub repetition: Repetition,
}

impl FieldType {
    pub fn json_type(&self) -> Value {
        match self.repetition {
            Repetition::Repeated => {
                json!({
                    "type": "array",
                    "items": self.primitive.json_type()
                })
            }
            _ => self.primitive.json_type(),
        }
    }
}

// ---------------------------------------------------------------------------
// Parsed default value  (from AST literals)
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, PartialEq)]
pub enum DefaultValue {
    None,
    Bool(bool),
    Integer(i64),
    Float(f64),
    String(std::string::String),
    List(Vec<DefaultValue>),
    Dict(Vec<(DefaultValue, DefaultValue)>),
    Set(Vec<DefaultValue>),
}

impl DefaultValue {
    /// Convert to a `serde_json::Value` for embedding in the schema.
    pub fn to_json(&self) -> Value {
        match self {
            Self::None => Value::Null,
            Self::Bool(b) => Value::Bool(*b),
            Self::Integer(n) => json!(n),
            Self::Float(f) => json!(f),
            Self::String(s) => Value::String(s.clone()),
            Self::List(items) => Value::Array(items.iter().map(|v| v.to_json()).collect()),
            Self::Dict(pairs) => {
                let mut map = serde_json::Map::new();
                for (k, v) in pairs {
                    // JSON keys must be strings — coerce
                    let key = match k {
                        Self::String(s) => s.clone(),
                        other => other.to_json().to_string(),
                    };
                    map.insert(key, v.to_json());
                }
                Value::Object(map)
            }
            Self::Set(items) => Value::Array(items.iter().map(|v| v.to_json()).collect()),
        }
    }
}

// ---------------------------------------------------------------------------
// Input field  (one parameter of predict/train)
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
pub struct InputField {
    pub name: std::string::String,
    /// Positional order in the function signature (0-based, excludes `self`).
    pub order: usize,
    pub field_type: FieldType,
    pub default: Option<DefaultValue>,
    pub description: Option<std::string::String>,
    pub ge: Option<f64>,
    pub le: Option<f64>,
    pub min_length: Option<u64>,
    pub max_length: Option<u64>,
    pub regex: Option<std::string::String>,
    pub choices: Option<Vec<DefaultValue>>,
    pub deprecated: Option<bool>,
}

impl InputField {
    /// Is this field required in the schema?
    pub fn is_required(&self) -> bool {
        self.default.is_none()
            && matches!(
                self.field_type.repetition,
                Repetition::Required | Repetition::Repeated
            )
    }
}

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OutputKind {
    Single,
    List,
    Iterator,
    ConcatenateIterator,
    Object,
}

#[derive(Debug, Clone)]
pub struct OutputType {
    pub kind: OutputKind,
    /// Element type for Single/List/Iterator/ConcatIterator.
    pub primitive: Option<PrimitiveType>,
    /// Fields for Object output (BaseModel subclass).
    pub fields: Option<IndexMap<std::string::String, ObjectField>>,
}

#[derive(Debug, Clone)]
pub struct ObjectField {
    pub field_type: FieldType,
    pub default: Option<DefaultValue>,
}

impl OutputType {
    pub fn json_type(&self) -> Value {
        match self.kind {
            OutputKind::Single => {
                let mut v = self.element_json_type();
                if let Value::Object(ref mut m) = v {
                    m.insert("title".into(), json!("Output"));
                }
                v
            }
            OutputKind::List => {
                json!({
                    "title": "Output",
                    "type": "array",
                    "items": self.element_json_type()
                })
            }
            OutputKind::Iterator => {
                json!({
                    "title": "Output",
                    "type": "array",
                    "items": self.element_json_type(),
                    "x-cog-array-type": "iterator"
                })
            }
            OutputKind::ConcatenateIterator => {
                json!({
                    "title": "Output",
                    "type": "array",
                    "items": self.element_json_type(),
                    "x-cog-array-type": "iterator",
                    "x-cog-array-display": "concatenate"
                })
            }
            OutputKind::Object => {
                let fields = match self.fields.as_ref() {
                    Some(f) => f,
                    None => return json!({"title": "Output", "type": "object"}),
                };
                let mut properties = Map::new();
                let mut required = Vec::new();

                for (name, field) in fields {
                    let mut prop = field.field_type.json_type();
                    if let Value::Object(ref mut m) = prop {
                        m.insert("title".into(), json!(title_case(name)));
                        if field.field_type.repetition == Repetition::Optional {
                            m.insert("nullable".into(), json!(true));
                        }
                    }
                    // Required if no default and not optional
                    if field.default.is_none()
                        && field.field_type.repetition != Repetition::Optional
                    {
                        required.push(json!(name));
                    }
                    properties.insert(name.clone(), prop);
                }

                let mut schema = json!({
                    "title": "Output",
                    "type": "object",
                    "properties": properties,
                });
                if !required.is_empty()
                    && let Some(obj) = schema.as_object_mut()
                {
                    obj.insert("required".into(), Value::Array(required));
                }
                schema
            }
        }
    }

    fn element_json_type(&self) -> Value {
        self.primitive
            .map(|p| p.json_type())
            .unwrap_or_else(|| json!({"type": "object"}))
    }
}

// ---------------------------------------------------------------------------
// Predictor info  (the top-level extraction result)
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    Predict,
    Train,
}

#[derive(Debug, Clone)]
pub struct PredictorInfo {
    pub inputs: IndexMap<std::string::String, InputField>,
    pub output: OutputType,
    pub mode: Mode,
}

// ---------------------------------------------------------------------------
// Type annotation AST  (intermediate, before resolution to FieldType)
// ---------------------------------------------------------------------------

/// Parsed type annotation from the Python AST — not yet resolved to our type system.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TypeAnnotation {
    /// Simple name: `str`, `int`, `Path`, `MyModel`, etc.
    Simple(std::string::String),
    /// Generic: `Optional[str]`, `List[int]`, `Iterator[str]`, etc.
    Generic(std::string::String, Vec<TypeAnnotation>),
    /// Union: `str | None`, `int | str`, etc.
    Union(Vec<TypeAnnotation>),
}

/// Import context — tracks what names are imported from which modules.
#[derive(Debug, Clone, Default)]
pub struct ImportContext {
    /// Map from local name → (module, original_name).
    /// e.g. `Path` → `("cog", "Path")`, `Optional` → `("typing", "Optional")`
    pub names: IndexMap<std::string::String, (std::string::String, std::string::String)>,
}

impl ImportContext {
    pub fn is_cog_type(&self, name: &str) -> bool {
        self.names
            .get(name)
            .is_some_and(|(module, _)| module == "cog")
    }

    pub fn is_typing_type(&self, name: &str) -> bool {
        self.names
            .get(name)
            .is_some_and(|(module, _)| module == "typing" || module == "typing_extensions")
    }

    pub fn is_base_model(&self, name: &str) -> bool {
        self.names
            .get(name)
            .is_some_and(|(module, orig)| module == "cog" && orig == "BaseModel")
    }

    pub fn is_base_predictor(&self, name: &str) -> bool {
        self.names
            .get(name)
            .is_some_and(|(module, orig)| module == "cog" && orig == "BasePredictor")
    }
}

/// Resolve a `TypeAnnotation` into a `FieldType` using the import context.
pub fn resolve_field_type(ann: &TypeAnnotation, ctx: &ImportContext) -> Result<FieldType> {
    match ann {
        TypeAnnotation::Simple(name) => {
            let prim = resolve_primitive(name, ctx)?;
            Ok(FieldType {
                primitive: prim,
                repetition: Repetition::Required,
            })
        }
        TypeAnnotation::Generic(outer, args) => {
            let outer_name = outer.as_str();

            // Optional[X] → X with Optional repetition
            if outer_name == "Optional" {
                if args.len() != 1 {
                    return Err(SchemaError::UnsupportedType(format!(
                        "Optional expects exactly 1 type argument, got {}",
                        args.len()
                    )));
                }
                let inner = resolve_field_type(&args[0], ctx)?;
                Ok(FieldType {
                    primitive: inner.primitive,
                    repetition: Repetition::Optional,
                })
            }
            // List[X] or list[X] → X with Repeated repetition
            else if outer_name == "List" || outer_name == "list" {
                if args.len() != 1 {
                    return Err(SchemaError::UnsupportedType(format!(
                        "List expects exactly 1 type argument, got {}",
                        args.len()
                    )));
                }
                let inner = resolve_field_type(&args[0], ctx)?;
                if inner.repetition != Repetition::Required {
                    return Err(SchemaError::UnsupportedType(
                        "nested generics like List[Optional[X]] are not supported".into(),
                    ));
                }
                Ok(FieldType {
                    primitive: inner.primitive,
                    repetition: Repetition::Repeated,
                })
            }
            // Anything else generic — not supported as input
            else {
                Err(SchemaError::UnsupportedType(format!(
                    "{outer_name}[...] is not a supported input type"
                )))
            }
        }
        TypeAnnotation::Union(members) => {
            // Only support X | None (equivalent to Optional[X])
            if members.len() == 2 {
                let has_none = members
                    .iter()
                    .any(|m| matches!(m, TypeAnnotation::Simple(n) if n == "None"));
                if has_none {
                    // We confirmed has_none is true and len is 2, so the other member exists.
                    let non_none = match members
                        .iter()
                        .find(|m| !matches!(m, TypeAnnotation::Simple(n) if n == "None"))
                    {
                        Some(m) => m,
                        None => {
                            return Err(SchemaError::UnsupportedType(
                                "union with only None types".into(),
                            ));
                        }
                    };
                    let inner = resolve_field_type(non_none, ctx)?;
                    return Ok(FieldType {
                        primitive: inner.primitive,
                        repetition: Repetition::Optional,
                    });
                }
            }
            Err(SchemaError::UnsupportedType(
                "union types other than X | None are not supported".to_string(),
            ))
        }
    }
}

/// Resolve an output type annotation.
pub fn resolve_output_type(
    ann: &TypeAnnotation,
    ctx: &ImportContext,
    model_classes: &ModelClassMap,
) -> Result<OutputType> {
    match ann {
        TypeAnnotation::Simple(name) => {
            // Check if it's a BaseModel subclass defined in the file
            if let Some(fields) = model_classes.get(name.as_str()) {
                let mut object_fields = IndexMap::new();
                for (fname, ftype, fdefault) in fields {
                    let ft = resolve_field_type(ftype, ctx)?;
                    object_fields.insert(
                        fname.clone(),
                        ObjectField {
                            field_type: ft,
                            default: fdefault.clone(),
                        },
                    );
                }
                return Ok(OutputType {
                    kind: OutputKind::Object,
                    primitive: None,
                    fields: Some(object_fields),
                });
            }
            // Otherwise it's a primitive
            if name == "Any" {
                return Ok(OutputType {
                    kind: OutputKind::Single,
                    primitive: Some(PrimitiveType::Any),
                    fields: None,
                });
            }
            let prim = resolve_primitive(name, ctx)?;
            Ok(OutputType {
                kind: OutputKind::Single,
                primitive: Some(prim),
                fields: None,
            })
        }
        TypeAnnotation::Generic(outer, args) => {
            let outer_name = outer.as_str();
            if args.len() != 1 {
                return Err(SchemaError::UnsupportedType(format!(
                    "{outer_name} expects exactly 1 type argument"
                )));
            }
            let inner_prim = match &args[0] {
                TypeAnnotation::Simple(n) => resolve_primitive(n, ctx)?,
                _ => {
                    return Err(SchemaError::UnsupportedType(
                        "nested generics in output type are not supported".into(),
                    ));
                }
            };

            match outer_name {
                "Iterator" | "AsyncIterator" => Ok(OutputType {
                    kind: OutputKind::Iterator,
                    primitive: Some(inner_prim),
                    fields: None,
                }),
                "ConcatenateIterator" | "AsyncConcatenateIterator" => {
                    if inner_prim != PrimitiveType::String {
                        return Err(SchemaError::ConcatIteratorNotStr(format!("{inner_prim:?}")));
                    }
                    Ok(OutputType {
                        kind: OutputKind::ConcatenateIterator,
                        primitive: Some(inner_prim),
                        fields: None,
                    })
                }
                "List" | "list" => Ok(OutputType {
                    kind: OutputKind::List,
                    primitive: Some(inner_prim),
                    fields: None,
                }),
                "Optional" => Err(SchemaError::OptionalOutput),
                _ => Err(SchemaError::UnsupportedType(format!(
                    "{outer_name}[...] is not a supported output type"
                ))),
            }
        }
        TypeAnnotation::Union(members) => {
            // Check for Optional pattern — not allowed as output
            let has_none = members
                .iter()
                .any(|m| matches!(m, TypeAnnotation::Simple(n) if n == "None"));
            if has_none {
                return Err(SchemaError::OptionalOutput);
            }
            Err(SchemaError::UnsupportedType(
                "union types are not supported as output".into(),
            ))
        }
    }
}

fn resolve_primitive(name: &str, _ctx: &ImportContext) -> Result<PrimitiveType> {
    PrimitiveType::from_name(name).ok_or_else(|| SchemaError::UnsupportedType(name.to_string()))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn title_case(s: &str) -> String {
    s.split('_')
        .map(|word| {
            let mut chars = word.chars();
            match chars.next() {
                None => String::new(),
                Some(c) => c.to_uppercase().to_string() + chars.as_str(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_primitive_json_types() {
        assert_eq!(PrimitiveType::Bool.json_type(), json!({"type": "boolean"}));
        assert_eq!(PrimitiveType::Float.json_type(), json!({"type": "number"}));
        assert_eq!(
            PrimitiveType::Integer.json_type(),
            json!({"type": "integer"})
        );
        assert_eq!(PrimitiveType::String.json_type(), json!({"type": "string"}));
        assert_eq!(
            PrimitiveType::Path.json_type(),
            json!({"type": "string", "format": "uri"})
        );
        assert_eq!(
            PrimitiveType::Secret.json_type(),
            json!({"type": "string", "format": "password", "writeOnly": true, "x-cog-secret": true})
        );
    }

    #[test]
    fn test_field_type_repeated() {
        let ft = FieldType {
            primitive: PrimitiveType::Integer,
            repetition: Repetition::Repeated,
        };
        assert_eq!(
            ft.json_type(),
            json!({"type": "array", "items": {"type": "integer"}})
        );
    }

    #[test]
    fn test_resolve_optional_union() {
        let ctx = ImportContext::default();
        let ann = TypeAnnotation::Union(vec![
            TypeAnnotation::Simple("str".into()),
            TypeAnnotation::Simple("None".into()),
        ]);
        let ft = resolve_field_type(&ann, &ctx).unwrap();
        assert_eq!(ft.primitive, PrimitiveType::String);
        assert_eq!(ft.repetition, Repetition::Optional);
    }

    #[test]
    fn test_title_case() {
        assert_eq!(title_case("hello_world"), "Hello World");
        assert_eq!(title_case("segmented_image"), "Segmented Image");
        assert_eq!(title_case("name"), "Name");
    }
}
