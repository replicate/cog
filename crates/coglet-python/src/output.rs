//! Output processing for prediction results.
//!
//! Handles conversion of Python output values to JSON-serializable format:
//! - Path objects are converted to base64 data URLs (sync mode)
//! - Dataclass/object outputs are serialized to dicts
//! - Generators are iterated and collected
//! - numpy arrays are converted to lists
//!
//! This replicates the behavior from cog's json.py:
//! - `make_encodeable()` normalizes output for JSON serialization
//! - `upload_files()` converts Path/file objects to URLs

use pyo3::prelude::*;

/// Process prediction output for JSON serialization.
///
/// This calls cog.json.make_encodeable() followed by cog.json.upload_files()
/// to convert the output to a JSON-serializable format with base64 data URLs
/// for any file outputs.
///
/// For sync mode (no output_file_prefix), files are base64 encoded.
/// For async mode, files would be uploaded to signed URLs (not yet implemented).
pub fn process_output<'py>(
    py: Python<'py>,
    output: &Bound<'py, PyAny>,
    _output_file_prefix: Option<&str>,
) -> PyResult<Bound<'py, PyAny>> {
    let cog_json = py.import("cog.json")?;
    let cog_files = py.import("cog.files")?;

    // First, make the output encodeable (handles Pydantic models, numpy, etc.)
    let make_encodeable = cog_json.getattr("make_encodeable")?;
    let encodeable = make_encodeable.call1((output,))?;

    // Then, upload/encode any files
    // For sync mode, upload_file converts to base64 data URLs
    let upload_files = cog_json.getattr("upload_files")?;
    let upload_file = cog_files.getattr("upload_file")?;

    // upload_files(obj, upload_file_fn) -> obj with files converted to URLs
    let result = upload_files.call1((&encodeable, &upload_file))?;

    Ok(result)
}

/// Process a single output item (for generator outputs).
pub fn process_output_item<'py>(
    py: Python<'py>,
    item: &Bound<'py, PyAny>,
) -> PyResult<Bound<'py, PyAny>> {
    process_output(py, item, None)
}
