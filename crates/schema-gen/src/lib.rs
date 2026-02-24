pub mod error;
pub mod parser;
pub mod schema;
pub mod types;

use crate::error::SchemaError;
use crate::parser::parse_predictor;
use crate::schema::{fix_nullable_anyof, generate_openapi_schema, remove_title_next_to_ref};
use crate::types::Mode;

/// High-level API: parse a Python source file and produce the OpenAPI JSON schema.
///
/// `source`: the full Python source code
/// `predict_ref`: the class or function name (e.g. "Predictor", "predict")
/// `mode`: predict or train
pub fn generate_schema(
    source: &str,
    predict_ref: &str,
    mode: Mode,
) -> Result<serde_json::Value, SchemaError> {
    let info = parse_predictor(source, predict_ref, mode)?;
    let mut schema = generate_openapi_schema(&info);
    remove_title_next_to_ref(&mut schema);
    fix_nullable_anyof(&mut schema);
    Ok(schema)
}
