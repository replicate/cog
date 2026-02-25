//! Input validation against the OpenAPI schema.
//!
//! Validates prediction inputs before dispatching to the Python worker,
//! catching missing required fields and unknown fields early with clear
//! error messages (matching the format users expect from pydantic).

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
}

impl InputValidator {
    /// Build a validator from a full OpenAPI schema document.
    ///
    /// Extracts `components.schemas.Input`, injects `additionalProperties: false`
    /// (for pydantic parity), and compiles a JSON Schema validator.
    ///
    /// Returns None if the schema doesn't contain an Input component.
    pub fn from_openapi_schema(schema: &Value) -> Option<Self> {
        let input_schema = schema.get("components")?.get("schemas")?.get("Input")?;

        let properties: HashSet<String> = input_schema
            .get("properties")
            .and_then(|p| p.as_object())
            .map(|obj| obj.keys().cloned().collect())
            .unwrap_or_default();

        let required: Vec<String> = input_schema
            .get("required")
            .and_then(|r| r.as_array())
            .map(|a| a.iter().filter_map(|v| v.as_str().map(String::from)).collect())
            .unwrap_or_default();

        // Clone and inject additionalProperties: false for pydantic parity
        let mut schema = input_schema.clone();
        if let Some(obj) = schema.as_object_mut() {
            obj.insert("additionalProperties".to_string(), Value::Bool(false));
        }

        let validator = jsonschema::validator_for(&schema)
            .inspect_err(|e| {
                tracing::warn!(error = %e, "Failed to compile input schema validator");
            })
            .ok()?;

        Some(Self {
            validator,
            properties,
            required,
        })
    }

    pub fn required_count(&self) -> usize {
        self.required.len()
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
        let mut seen_additional = false;

        for error in self.validator.iter_errors(input) {
            let msg = error.to_string();

            // "required" errors: emit one entry per missing field
            if msg.contains("is a required property") && !seen_required {
                seen_required = true;
                let input_obj = input.as_object();
                for field in &self.required {
                    let present = input_obj.map(|obj| obj.contains_key(field)).unwrap_or(false);
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

            // "additionalProperties" errors: emit one entry per unknown field
            if msg.contains("Additional properties") && !seen_additional {
                seen_additional = true;
                if let Some(input_obj) = input.as_object() {
                    for key in input_obj.keys() {
                        if !self.properties.contains(key) {
                            errors.push(ValidationError {
                                field: key.clone(),
                                msg: format!("Unexpected field '{key}'"),
                                error_type: "value_error.extra".to_string(),
                            });
                        }
                    }
                }
                continue;
            }

            // Skip duplicate required/additional messages
            if seen_required && msg.contains("is a required property") {
                continue;
            }
            if seen_additional && msg.contains("Additional properties") {
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
    fn rejects_additional_properties() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        // Extra field should fail
        let errs = validator.validate(&json!({"s": "hello", "extra": "bad"})).unwrap_err();
        assert_eq!(errs.len(), 1);
        assert_eq!(errs[0].field, "extra");
        assert!(errs[0].msg.contains("Unexpected"));
    }

    #[test]
    fn missing_and_extra_fields() {
        let schema = make_schema(json!({
            "type": "object",
            "properties": {
                "s": {"type": "string", "title": "S"}
            },
            "required": ["s"]
        }));

        let validator = InputValidator::from_openapi_schema(&schema).unwrap();

        // wrong=value with missing s
        let errs = validator.validate(&json!({"wrong": "value"})).unwrap_err();
        assert!(errs.len() >= 2);
        let fields: Vec<&str> = errs.iter().map(|e| e.field.as_str()).collect();
        assert!(fields.contains(&"s"), "should report missing s: {fields:?}");
        assert!(fields.contains(&"wrong"), "should report extra wrong: {fields:?}");
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

        let errs = validator.validate(&json!({"count": "not_a_number"})).unwrap_err();
        assert_eq!(errs[0].field, "count");
    }

    #[test]
    fn no_schema_returns_none() {
        let schema = json!({"components": {"schemas": {}}});
        assert!(InputValidator::from_openapi_schema(&schema).is_none());
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
}
