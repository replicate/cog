//! OpenAPI 3.0.2 schema generation from `PredictorInfo`.
//!
//! Mirrors `python/cog/_schemas.py`. Produces a semantically equivalent
//! OpenAPI specification to the Python runtime generator.

use serde_json::{Map, Value, json};

use crate::types::*;

/// Generate a complete OpenAPI 3.0.2 specification from predictor info.
pub fn generate_openapi_schema(info: &PredictorInfo) -> Value {
    let (input_schema, enum_schemas) = build_input_schema(info);
    let output_schema = info.output.json_type();

    let is_train = info.mode == Mode::Train;
    let (endpoint, request_name, response_name, cancel_endpoint, summary, description, op_id, cancel_op_id) = if is_train {
        (
            "/trainings",
            "TrainingRequest",
            "TrainingResponse",
            "/trainings/{training_id}/cancel",
            "Train",
            "Run a single training on the model",
            "train_trainings_post",
            "cancel_trainings__training_id__cancel_post",
        )
    } else {
        (
            "/predictions",
            "PredictionRequest",
            "PredictionResponse",
            "/predictions/{prediction_id}/cancel",
            "Predict",
            "Run a single prediction on the model",
            "predict_predictions_post",
            "cancel_predictions__prediction_id__cancel_post",
        )
    };

    let cancel_param_name = if is_train {
        "training_id"
    } else {
        "prediction_id"
    };

    let mut components: Map<String, Value> = Map::new();

    // Input schema
    components.insert("Input".into(), input_schema);

    // Output schema
    components.insert("Output".into(), output_schema);

    // Enum schemas (for choices)
    for (name, schema) in &enum_schemas {
        components.insert(name.clone(), schema.clone());
    }

    // Request schema
    components.insert(
        request_name.into(),
        json!({
            "title": request_name,
            "type": "object",
            "properties": {
                "id": {"title": "Id", "type": "string"},
                "input": {"$ref": "#/components/schemas/Input"}
            }
        }),
    );

    // Response schema
    components.insert(
        response_name.into(),
        json!({
            "title": response_name,
            "type": "object",
            "properties": {
                "input": {"$ref": "#/components/schemas/Input"},
                "output": {"$ref": "#/components/schemas/Output"},
                "id": {"title": "Id", "type": "string"},
                "version": {"title": "Version", "type": "string"},
                "created_at": {"title": "Created At", "type": "string", "format": "date-time"},
                "started_at": {"title": "Started At", "type": "string", "format": "date-time"},
                "completed_at": {"title": "Completed At", "type": "string", "format": "date-time"},
                "status": {"title": "Status", "type": "string"},
                "error": {"title": "Error", "type": "string"},
                "logs": {"title": "Logs", "type": "string"},
                "metrics": {"title": "Metrics", "type": "object"}
            }
        }),
    );

    // Status enum
    components.insert(
        "Status".into(),
        json!({
            "title": "Status",
            "description": "An enumeration.",
            "enum": ["starting", "processing", "succeeded", "canceled", "failed"],
            "type": "string"
        }),
    );

    // Validation error schemas
    components.insert(
        "HTTPValidationError".into(),
        json!({
            "title": "HTTPValidationError",
            "type": "object",
            "properties": {
                "detail": {
                    "title": "Detail",
                    "type": "array",
                    "items": {"$ref": "#/components/schemas/ValidationError"}
                }
            }
        }),
    );

    components.insert(
        "ValidationError".into(),
        json!({
            "title": "ValidationError",
            "required": ["loc", "msg", "type"],
            "type": "object",
            "properties": {
                "loc": {
                    "title": "Location",
                    "type": "array",
                    "items": {"anyOf": [{"type": "string"}, {"type": "integer"}]}
                },
                "msg": {"title": "Message", "type": "string"},
                "type": {"title": "Error Type", "type": "string"}
            }
        }),
    );

    // Assemble full spec
    let request_ref = format!("#/components/schemas/{request_name}");
    let response_ref = format!("#/components/schemas/{response_name}");

    json!({
        "openapi": "3.0.2",
        "info": {"title": "Cog", "version": "0.1.0"},
        "paths": {
            "/": {
                "get": {
                    "summary": "Root",
                    "operationId": "root__get",
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {"application/json": {"schema": {}}}
                        }
                    }
                }
            },
            "/health-check": {
                "get": {
                    "summary": "Healthcheck",
                    "operationId": "healthcheck_health_check_get",
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {"application/json": {"schema": {}}}
                        }
                    }
                }
            },
            (endpoint): {
                "post": {
                    "summary": summary,
                    "description": description,
                    "operationId": op_id,
                    "requestBody": {
                        "content": {
                            "application/json": {
                                "schema": {"$ref": request_ref}
                            }
                        }
                    },
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {"application/json": {"schema": {"$ref": response_ref}}}
                        },
                        "422": {
                            "description": "Validation Error",
                            "content": {"application/json": {"schema": {"$ref": "#/components/schemas/HTTPValidationError"}}}
                        }
                    }
                }
            },
            (cancel_endpoint): {
                "post": {
                    "summary": "Cancel",
                    "operationId": cancel_op_id,
                    "parameters": [{
                        "required": true,
                        "schema": {"title": title_case_single(cancel_param_name), "type": "string"},
                        "name": cancel_param_name,
                        "in": "path"
                    }],
                    "responses": {
                        "200": {
                            "description": "Successful Response",
                            "content": {"application/json": {"schema": {}}}
                        },
                        "422": {
                            "description": "Validation Error",
                            "content": {"application/json": {"schema": {"$ref": "#/components/schemas/HTTPValidationError"}}}
                        }
                    }
                }
            }
        },
        "components": {
            "schemas": components
        }
    })
}

// ---------------------------------------------------------------------------
// Input schema
// ---------------------------------------------------------------------------

/// Build the Input schema and any enum schemas for choices.
fn build_input_schema(info: &PredictorInfo) -> (Value, Vec<(String, Value)>) {
    let mut properties: Map<String, Value> = Map::new();
    let mut required: Vec<Value> = Vec::new();
    let mut enum_schemas: Vec<(String, Value)> = Vec::new();

    for (name, field) in &info.inputs {
        let mut prop: Map<String, Value> = Map::new();

        // x-order for field ordering
        prop.insert("x-order".into(), json!(field.order));

        if let Some(ref choices) = field.choices {
            // Choices → use allOf with $ref to enum schema
            let enum_name = title_case_single(name);
            let enum_type = field.field_type.primitive.json_type();
            let type_str = enum_type
                .get("type")
                .and_then(|v| v.as_str())
                .unwrap_or("string");

            let choice_values: Vec<Value> = choices.iter().map(|c| c.to_json()).collect();

            enum_schemas.push((
                enum_name.clone(),
                json!({
                    "title": &enum_name,
                    "description": "An enumeration.",
                    "enum": choice_values,
                    "type": type_str
                }),
            ));

            prop.insert(
                "allOf".into(),
                json!([{"$ref": format!("#/components/schemas/{enum_name}")}]),
            );
        } else {
            // Regular field — inline type
            prop.insert("title".into(), json!(title_case_words(name)));
            let type_schema = field.field_type.json_type();
            if let Value::Object(m) = type_schema {
                for (k, v) in m {
                    prop.insert(k, v);
                }
            }
        }

        // Required?
        if field.is_required() {
            required.push(json!(name));
        }

        // Default value
        if let Some(ref default) = field.default {
            prop.insert("default".into(), default.to_json());
        }

        // Nullable
        if field.field_type.repetition == Repetition::Optional {
            prop.insert("nullable".into(), json!(true));
        }

        // Description
        if let Some(ref desc) = field.description {
            prop.insert("description".into(), json!(desc));
        }

        // Numeric constraints
        if let Some(ge) = field.ge {
            prop.insert("minimum".into(), json!(ge));
        }
        if let Some(le) = field.le {
            prop.insert("maximum".into(), json!(le));
        }

        // String constraints
        if let Some(min_len) = field.min_length {
            prop.insert("minLength".into(), json!(min_len));
        }
        if let Some(max_len) = field.max_length {
            prop.insert("maxLength".into(), json!(max_len));
        }
        if let Some(ref regex) = field.regex {
            prop.insert("pattern".into(), json!(regex));
        }

        // Deprecated
        if let Some(deprecated) = field.deprecated {
            if deprecated {
                prop.insert("deprecated".into(), json!(true));
            }
        }

        properties.insert(name.clone(), Value::Object(prop));
    }

    let mut input_schema = json!({
        "title": "Input",
        "type": "object",
        "properties": properties,
    });

    if !required.is_empty() {
        if let Some(obj) = input_schema.as_object_mut() {
            obj.insert("required".into(), Value::Array(required));
        }
    }

    (input_schema, enum_schemas)
}

// ---------------------------------------------------------------------------
// Post-processing (mirrors openapi_schema.py fixups)
// ---------------------------------------------------------------------------

/// Remove `title` from any object that also has `$ref`.
/// OpenAPI 3.0 doesn't allow sibling keywords next to `$ref`.
pub fn remove_title_next_to_ref(schema: &mut Value) {
    match schema {
        Value::Object(map) => {
            if map.contains_key("$ref") {
                map.remove("title");
            }
            for (_, v) in map.iter_mut() {
                remove_title_next_to_ref(v);
            }
        }
        Value::Array(arr) => {
            for v in arr.iter_mut() {
                remove_title_next_to_ref(v);
            }
        }
        _ => {}
    }
}

/// Convert `{"anyOf": [{"type": T}, {"type": "null"}]}` → `{"type": T, "nullable": true}`.
/// OpenAPI 3.0 uses `nullable` instead of union-with-null.
pub fn fix_nullable_anyof(schema: &mut Value) {
    match schema {
        Value::Object(map) => {
            // First recurse into children
            for (_, v) in map.iter_mut() {
                fix_nullable_anyof(v);
            }

            // Then check for anyOf with null pattern
            if let Some(any_of) = map.get("anyOf") {
                if let Value::Array(variants) = any_of {
                    if variants.len() == 2 {
                        let has_null = variants.iter().any(|v| {
                            v.get("type").and_then(|t| t.as_str()) == Some("null")
                        });
                        if has_null {
                            if let Some(non_null) = variants.iter().find(|v| {
                                v.get("type").and_then(|t| t.as_str()) != Some("null")
                            }) {
                                let mut merged = non_null.clone();
                                if let Value::Object(ref mut m) = merged {
                                    m.insert("nullable".into(), json!(true));
                                }
                                *schema = merged;
                            }
                        }
                    }
                }
            }
        }
        Value::Array(arr) => {
            for v in arr.iter_mut() {
                fix_nullable_anyof(v);
            }
        }
        _ => {}
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Title case a single word/identifier: `prediction_id` → `Prediction Id`
fn title_case_single(s: &str) -> String {
    let mut chars = s.chars();
    match chars.next() {
        None => String::new(),
        Some(c) => c.to_uppercase().to_string() + chars.as_str(),
    }
}

/// Title case with underscore splitting: `segmented_image` → `Segmented Image`
fn title_case_words(s: &str) -> String {
    s.split('_')
        .map(|word| title_case_single(word))
        .collect::<Vec<_>>()
        .join(" ")
}

#[cfg(test)]
mod tests {
    use super::*;
    use indexmap::IndexMap;

    fn simple_predictor() -> PredictorInfo {
        let mut inputs = IndexMap::new();
        inputs.insert(
            "s".into(),
            InputField {
                name: "s".into(),
                order: 0,
                field_type: FieldType {
                    primitive: PrimitiveType::String,
                    repetition: Repetition::Required,
                },
                default: None,
                description: None,
                ge: None,
                le: None,
                min_length: None,
                max_length: None,
                regex: None,
                choices: None,
                deprecated: None,
            },
        );

        PredictorInfo {
            inputs,
            output: OutputType {
                kind: OutputKind::Single,
                primitive: Some(PrimitiveType::String),
                fields: None,
            },
            mode: Mode::Predict,
        }
    }

    #[test]
    fn test_generates_valid_openapi() {
        let info = simple_predictor();
        let schema = generate_openapi_schema(&info);

        assert_eq!(schema["openapi"], "3.0.2");
        assert_eq!(schema["info"]["title"], "Cog");
        assert!(schema["paths"]["/predictions"]["post"].is_object());
        assert!(schema["components"]["schemas"]["Input"].is_object());
        assert!(schema["components"]["schemas"]["Output"].is_object());
    }

    #[test]
    fn test_input_required_field() {
        let info = simple_predictor();
        let schema = generate_openapi_schema(&info);

        let input = &schema["components"]["schemas"]["Input"];
        assert_eq!(input["required"], json!(["s"]));
    }

    #[test]
    fn test_train_mode_endpoints() {
        let mut info = simple_predictor();
        info.mode = Mode::Train;
        let schema = generate_openapi_schema(&info);

        assert!(schema["paths"]["/trainings"]["post"].is_object());
        assert!(schema["components"]["schemas"]["TrainingRequest"].is_object());
    }

    #[test]
    fn test_choices_generate_enum() {
        let mut inputs = IndexMap::new();
        inputs.insert(
            "color".into(),
            InputField {
                name: "color".into(),
                order: 0,
                field_type: FieldType {
                    primitive: PrimitiveType::String,
                    repetition: Repetition::Required,
                },
                default: None,
                description: None,
                ge: None,
                le: None,
                min_length: None,
                max_length: None,
                regex: None,
                choices: Some(vec![
                    DefaultValue::String("red".into()),
                    DefaultValue::String("blue".into()),
                ]),
                deprecated: None,
            },
        );

        let info = PredictorInfo {
            inputs,
            output: OutputType {
                kind: OutputKind::Single,
                primitive: Some(PrimitiveType::String),
                fields: None,
            },
            mode: Mode::Predict,
        };

        let schema = generate_openapi_schema(&info);
        let color_enum = &schema["components"]["schemas"]["Color"];
        assert_eq!(color_enum["enum"], json!(["red", "blue"]));
    }

    #[test]
    fn test_remove_title_next_to_ref() {
        let mut schema = json!({
            "title": "Foo",
            "$ref": "#/components/schemas/Bar"
        });
        remove_title_next_to_ref(&mut schema);
        assert!(schema.get("title").is_none());
        assert!(schema.get("$ref").is_some());
    }
}
