//! Input validation against the OpenAPI schema.
//!
//! Validates prediction inputs before dispatching to the Python worker.
//! Strips unknown fields silently and catches missing required fields
//! with clear error messages (matching the format users expect from pydantic).

use std::collections::HashSet;

use serde_json::Value;

/// A single validation error for one field.
#[derive(Debug)]
pub struct ValidationError {
    /// Field name (used as loc[2] in the pydantic-compatible response).
    pub field: String,
    /// Human-readable error message.
    pub msg: String,
    /// Error type string (e.g. "value_error.missing").
    pub error_type: String,
}

/// Compiled input validator built from the OpenAPI schema's Input component.
pub struct InputValidator {
    validator: jsonschema::Validator,
    /// Known property names from the schema.
    properties: HashSet<String>,
    /// Required field names from the schema.
    required: Vec<String>,
    /// Properties that should resolve to `null` when omitted from the request.
    ///
    /// These are inputs declared `Optional[T]` / `T | None` that carry no
    /// real default (schema: `nullable: true`, absent from `required`, no
    /// `default` key). The Python signature may not have a usable
    /// `__defaults__` entry for these (e.g. a bare `value: Optional[str]`),
    /// so omitting them would raise `TypeError: missing 1 required positional
    /// argument` in the worker. Injecting an explicit `null` makes the
    /// documented behaviour ("optional inputs default to None") hold, with the
    /// generated schema staying the source of truth for what is optional.
    ///
    /// This rule mirrors the schema-generation discriminator in
    /// `pkg/schema/openapi.go` (`buildInputSchema`), which emits `nullable`,
    /// `required` membership, and the `default` key independently. The unit
    /// tests below hand-build schema JSON, so they cannot catch drift if that
    /// generator changes shape (e.g. `anyOf`-with-null instead of
    /// `nullable: true`). The end-to-end guard against such drift is the
    /// integration test `integration-tests/tests/optional_input_no_default.txtar`,
    /// which runs the real generator through coglet.
    inject_null: Vec<String>,
}

impl InputValidator {
    /// Build a validator from a full OpenAPI schema document.
    ///
    /// Extracts `components.schemas.Input` and compiles a JSON Schema validator.
    /// Unknown input fields should be stripped via `strip_unknown()` before
    /// calling `validate()`.
    ///
    /// Returns None if the schema doesn't contain an Input component.
    pub fn from_openapi_schema(schema: &Value) -> Option<Self> {
        Self::from_openapi_schema_key(schema, "Input")
    }

    /// Build a validator from a full OpenAPI schema document using a custom
    /// schema key (e.g. "TrainingInput" for train endpoints).
    ///
    /// Returns None if the schema doesn't contain the specified component.
    pub fn from_openapi_schema_key(schema: &Value, key: &str) -> Option<Self> {
        let input_schema = schema.get("components")?.get("schemas")?.get(key)?;

        let properties: HashSet<String> = input_schema
            .get("properties")
            .and_then(|p| p.as_object())
            .map(|obj| obj.keys().cloned().collect())
            .unwrap_or_default();

        let required: Vec<String> = input_schema
            .get("required")
            .and_then(|r| r.as_array())
            .map(|a| {
                a.iter()
                    .filter_map(|v| v.as_str().map(String::from))
                    .collect()
            })
            .unwrap_or_default();

        // Properties that should resolve to `null` when omitted: nullable,
        // not required, and with no `default` key. This mirrors the
        // schema-generation discriminator in pkg/schema/openapi.go, where
        // `nullable`, membership in `required`, and the presence of a
        // `default` key are emitted independently.
        let required_set: HashSet<&str> = required.iter().map(String::as_str).collect();
        let inject_null: Vec<String> = input_schema
            .get("properties")
            .and_then(|p| p.as_object())
            .map(|obj| {
                obj.iter()
                    .filter(|(name, prop)| {
                        let nullable = prop
                            .get("nullable")
                            .and_then(Value::as_bool)
                            .unwrap_or(false);
                        let has_default = prop.get("default").is_some();
                        nullable && !has_default && !required_set.contains(name.as_str())
                    })
                    .map(|(name, _)| name.clone())
                    .collect()
            })
            .unwrap_or_default();

        let mut resolved = input_schema.clone();

        // Inline $ref pointers so the validator can resolve them without
        // the full OpenAPI document context. cog-schema-gen emits $ref for
        // enum choices (e.g. "#/components/schemas/Color").
        let all_schemas = schema.get("components").and_then(|c| c.get("schemas"));
        inline_refs(&mut resolved, all_schemas);

        let validator = jsonschema::validator_for(&resolved)
            .inspect_err(|e| {
                tracing::warn!(error = %e, "Failed to compile input schema validator");
            })
            .ok()?;

        Some(Self {
            validator,
            properties,
            required,
            inject_null,
        })
    }

    pub fn required_count(&self) -> usize {
        self.required.len()
    }

    /// Inject an explicit `null` for any optional-with-no-default property that
    /// is absent from the input.
    ///
    /// Only injects on true absence — a value already present (including an
    /// explicit `null`) is never overwritten. Call this only after
    /// `validate()` succeeds so missing *required* fields still error.
    pub fn inject_missing_optionals(&self, input: &mut Value) {
        if self.inject_null.is_empty() {
            return;
        }
        let Some(obj) = input.as_object_mut() else {
            return;
        };
        for name in &self.inject_null {
            if !obj.contains_key(name) {
                obj.insert(name.clone(), Value::Null);
            }
        }
    }

    /// Strip unknown input fields in place, returning the names of removed fields.
    pub fn strip_unknown(&self, input: &mut Value) -> Vec<String> {
        let Some(obj) = input.as_object_mut() else {
            return Vec::new();
        };
        let unknown_keys: Vec<String> = obj
            .keys()
            .filter(|k| !self.properties.contains(*k))
            .cloned()
            .collect();
        for key in &unknown_keys {
            obj.remove(key);
        }
        unknown_keys
    }

    /// Validate an input value against the schema.
    ///
    /// Returns Ok(()) on success, or a list of per-field validation errors
    /// formatted for the pydantic-compatible `detail` response.
    pub fn validate(&self, input: &Value) -> Result<(), Vec<ValidationError>> {
        if self.validator.validate(input).is_ok() {
            return Ok(());
        }

        let mut errors = Vec::new();
        let mut seen_required = false;

        for error in self.validator.iter_errors(input) {
            let msg = error.to_string();

            // "required" errors: emit one entry per missing field
            if msg.contains("is a required property") && !seen_required {
                seen_required = true;
                let input_obj = input.as_object();
                for field in &self.required {
                    let present = input_obj
                        .map(|obj| obj.contains_key(field))
                        .unwrap_or(false);
                    if !present {
                        errors.push(ValidationError {
                            field: field.clone(),
                            msg: "Field required".to_string(),
                            error_type: "value_error.missing".to_string(),
                        });
                    }
                }
                continue;
            }

            // Skip duplicate required messages
            if seen_required && msg.contains("is a required property") {
                continue;
            }

            // Type/constraint errors on specific fields
            let path = error.instance_path.to_string();
            let field = path.trim_start_matches('/');
            let field_name = if field.is_empty() {
                "__root__".to_string()
            } else {
                field.to_string()
            };
            errors.push(ValidationError {
                field: field_name,
                msg,
                error_type: "value_error".to_string(),
            });
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(errors)
        }
    }
}

/// Recursively inline `$ref` pointers in a JSON Schema value.
///
/// Resolves `{"$ref": "#/components/schemas/Foo"}` by looking up `Foo` in the
/// provided schemas map and replacing the `$ref` object with the referenced
/// content. This allows the validator to work on an extracted subschema without
/// needing the full OpenAPI document.
fn inline_refs(value: &mut Value, all_schemas: Option<&Value>) {
    match value {
        Value::Object(obj) => {
            // If this object is a $ref, resolve it
            if let Some(Value::String(ref_str)) = obj.get("$ref")
                && let Some(resolved) = resolve_ref(ref_str, all_schemas)
            {
                *value = resolved;
                // Recurse into the resolved value (it may contain more $refs)
                inline_refs(value, all_schemas);
                return;
            }
            // Recurse into all values
            for v in obj.values_mut() {
                inline_refs(v, all_schemas);
            }
        }
        Value::Array(arr) => {
            for v in arr.iter_mut() {
                inline_refs(v, all_schemas);
            }
        }
        _ => {}
    }
}

/// Resolve a `$ref` string like `#/components/schemas/Foo` against the schemas map.
fn resolve_ref(ref_str: &str, all_schemas: Option<&Value>) -> Option<Value> {
    let name = ref_str.strip_prefix("#/components/schemas/")?;
    all_schemas?.get(name).cloned()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn make_schema(input_schema: Value) -> Value {
        json!({
            "components": {
                "schemas": {
                    "Input": input_schema
                }
            }
        })
    }

    #[test]
    fn validates_required_fields() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        // Valid input
        assert!(validator.validate(&json!({"s": "hello"})).is_ok());

        // Missing required field
        let errs = validator.validate(&json!({})).unwrap_err();
        assert_eq!(errs.len(), 1);
        assert_eq!(errs[0].field, "s");
        assert_eq!(errs[0].msg, "Field required");
    }

    #[test]
    fn allows_additional_properties_in_validate() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        // Extra fields should NOT cause validation failure — they get stripped separately
        assert!(
            validator
                .validate(&json!({"s": "hello", "extra": "bad"}))
                .is_ok(),
            "unknown inputs should not cause validation errors"
        );
    }

    #[test]
    fn strip_unknown_removes_extra_fields() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({"s": "hello", "guidance_scale": 7.5, "extra": "bad"});
        let stripped = validator.strip_unknown(&mut input);

        // Should have removed the unknown fields
        assert_eq!(stripped.len(), 2);
        assert!(stripped.contains(&"guidance_scale".to_string()));
        assert!(stripped.contains(&"extra".to_string()));

        // Known field should remain
        assert_eq!(input, json!({"s": "hello"}));
    }

    #[test]
    fn strip_unknown_preserves_known_fields() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"},
                "n": {"type": "integer"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({"s": "hello", "n": 42});
        let stripped = validator.strip_unknown(&mut input);

        assert!(stripped.is_empty());
        assert_eq!(input, json!({"s": "hello", "n": 42}));
    }

    #[test]
    fn strip_unknown_returns_empty_for_no_extra_fields() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({"s": "hello"});
        let stripped = validator.strip_unknown(&mut input);
        assert!(stripped.is_empty());
    }

    #[test]
    fn missing_required_with_extra_fields() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        // Strip unknowns first, then validate — only the missing required field
        // should be an error, not the extra field
        let mut input = json!({"wrong": "value"});
        let stripped = validator.strip_unknown(&mut input);
        assert_eq!(stripped, vec!["wrong".to_string()]);

        let errs = validator.validate(&input).unwrap_err();
        assert_eq!(errs.len(), 1);
        assert_eq!(errs[0].field, "s");
        assert_eq!(errs[0].msg, "Field required");
    }

    #[test]
    fn validates_types() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "count": {"type": "integer", "title": "Count"}
            },
            "required": ["count"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        assert!(validator.validate(&json!({"count": 5})).is_ok());

        let errs = validator
            .validate(&json!({"count": "not_a_number"}))
            .unwrap_err();
        assert_eq!(errs[0].field, "count");
    }

    #[test]
    fn no_schema_returns_none() {
        let schema = json!({"components": {"schemas": {}}});
        assert!(InputValidator::from_openapi_schema(&schema).is_none());
    }

    #[test]
    fn resolves_ref_for_choices() {
        let schema = json!({
            "components": {
                "schemas": {
                    "Input": {
                        "type": "object",
                        "properties": {
                            "color": {
                                "allOf": [{"$ref": "#/components/schemas/Color"}],
                                "x-order": 0
                            }
                        },
                        "required": ["color"]
                    },
                    "Color": {
                        "title": "Color",
                        "description": "An enumeration.",
                        "enum": ["red", "green", "blue"],
                        "type": "string"
                    }
                }
            }
        });

        let validator = InputValidator::from_openapi_schema(&schema);
        assert!(validator.is_some(), "validator should compile with $ref");

        let validator = validator.unwrap();
        assert!(validator.validate(&json!({"color": "red"})).is_ok());
        assert!(validator.validate(&json!({"color": "purple"})).is_err());
    }

    #[test]
    fn optional_fields_work() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string"},
                "n": {"type": "integer"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        assert!(validator.validate(&json!({"s": "hello"})).is_ok());
        assert!(validator.validate(&json!({"s": "hello", "n": 42})).is_ok());
    }

    #[test]
    fn injects_null_for_omitted_optional_without_default() {
        // `value: Optional[str]` / `value: Optional[str] = Input(description=...)`
        // emit: nullable, not required, no `default` key.
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"},
                "value": {"type": "string", "title": "Value", "nullable": true}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({"s": "hello"});
        validator.inject_missing_optionals(&mut input);
        assert_eq!(input, json!({"s": "hello", "value": null}));
    }

    #[test]
    fn does_not_override_present_optional_value() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "value": {"type": "string", "title": "Value", "nullable": true}
            }
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        // Present value (including explicit null) is never overwritten.
        let mut input = json!({"value": "present"});
        validator.inject_missing_optionals(&mut input);
        assert_eq!(input, json!({"value": "present"}));

        let mut input = json!({"value": null});
        validator.inject_missing_optionals(&mut input);
        assert_eq!(input, json!({"value": null}));
    }

    #[test]
    fn does_not_inject_for_optional_with_explicit_default() {
        // `Optional[str] = Input(default=None)` emits `default: null`;
        // `seed: int = Input(default=42)` emits `default: 42`. Python's
        // __defaults__ already resolves these, so injection must skip them.
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "opt_none": {"type": "string", "title": "OptNone", "nullable": true, "default": null},
                "with_default": {"type": "integer", "title": "WithDefault", "default": 42}
            }
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({});
        validator.inject_missing_optionals(&mut input);
        assert_eq!(
            input,
            json!({}),
            "fields with a default key are left absent"
        );
    }

    #[test]
    fn injects_null_for_omitted_optional_list() {
        // `Optional[list[str]]` / `list[str] | None` emits a top-level
        // `nullable: true` array with no default and is absent from `required`.
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {"type": "string"},
                    "title": "Items",
                    "nullable": true
                }
            }
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({});
        validator.inject_missing_optionals(&mut input);
        assert_eq!(input, json!({"items": null}));
    }

    #[test]
    fn injects_null_for_omitted_optional_enum() {
        // An optional choices field emits `allOf` + `$ref` with a sibling
        // `nullable: true` and no default. The discriminator reads `nullable`
        // at the property top level, so it should still inject.
        let schema = json!({
            "components": {
                "schemas": {
                    "Input": {
                        "type": "object",
                        "properties": {
                            "color": {
                                "allOf": [{"$ref": "#/components/schemas/Color"}],
                                "title": "Color",
                                "nullable": true,
                                "x-order": 0
                            }
                        }
                    },
                    "Color": {
                        "title": "Color",
                        "description": "An enumeration.",
                        "enum": ["red", "green", "blue"],
                        "type": "string"
                    }
                }
            }
        });

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({});
        validator.inject_missing_optionals(&mut input);
        assert_eq!(input, json!({"color": null}));
    }

    #[test]
    fn does_not_inject_for_required_field() {
        // A required field is never nullable-without-default in practice, but
        // guard against injecting for anything listed in `required`.
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "r": {"type": "string", "title": "R", "nullable": true}
            },
            "required": ["r"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        let mut input = json!({});
        validator.inject_missing_optionals(&mut input);
        assert_eq!(input, json!({}), "required fields are not injected");
    }
}
