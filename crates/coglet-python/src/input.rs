//! Input processing for cog predictors.
//!
//! This module handles file downloads for cog predictor inputs.
//! Input validation is performed at the HTTP edge using the OpenAPI schema;
//! the worker only needs to download URLPath inputs and pass them through.

use std::collections::HashSet;

use pyo3::prelude::*;
use pyo3::types::PyDict;

/// Type alias for Python object.
type PyObject = Py<PyAny>;

/// RAII wrapper for prepared input that cleans up temp files on drop.
///
/// When URLPath inputs are downloaded, they create temp files. This struct
/// ensures those files are cleaned up when the prediction completes (success,
/// failure, or cancellation).
pub struct PreparedInput {
    /// The prepared input dict (ready for predict(**kwargs))
    dict: Py<PyDict>,
    /// Paths to cleanup on drop (downloaded temp files)
    cleanup_paths: Vec<PyObject>,
}

impl PreparedInput {
    /// Create a new PreparedInput with the given dict and paths to cleanup.
    pub fn new(dict: Py<PyDict>, cleanup_paths: Vec<PyObject>) -> Self {
        Self {
            dict,
            cleanup_paths,
        }
    }

    /// Get the input dict bound to the given Python context.
    pub fn dict<'py>(&self, py: Python<'py>) -> Bound<'py, PyDict> {
        self.dict.bind(py).clone()
    }
}

impl Drop for PreparedInput {
    fn drop(&mut self) {
        if self.cleanup_paths.is_empty() {
            return;
        }

        Python::attach(|py| {
            for path in &self.cleanup_paths {
                let path_bound = path.bind(py);
                let kwargs = PyDict::new(py);
                if kwargs.set_item("missing_ok", true).is_ok()
                    && let Err(e) = path_bound.call_method("unlink", (), Some(&kwargs))
                {
                    tracing::warn!(error = %e, "Failed to cleanup temp file");
                }
            }
        });
    }
}

// Safety: PyObject is Send in PyO3 0.23+, we only access through Python::attach
unsafe impl Send for PreparedInput {}

/// Prepare input for prediction.
///
/// Coerces URL strings to the appropriate cog types based on the function's
/// type annotations: `File`-annotated params get `File.validate()` (IO-like),
/// `Path`-annotated params get `Path.validate()` (filesystem path + download).
/// Returns a PreparedInput that cleans up temp files on drop.
///
/// Input validation is handled at the HTTP edge via the OpenAPI schema —
/// this function only handles URL->Path/File coercion and file downloads.
///
/// `func` is the Python predict/train callable used to introspect type annotations.
pub fn prepare_input(
    py: Python<'_>,
    input: &Bound<'_, PyDict>,
    func: &Bound<'_, PyAny>,
) -> PyResult<PreparedInput> {
    let fields = classify_fields(py, func)?;
    coerce_url_strings(py, input, &fields)?;
    let cleanup_paths = download_url_paths_into_dict(py, input)?;
    Ok(PreparedInput::new(input.clone().unbind(), cleanup_paths))
}

/// Result of inspecting a Python function's type annotations for File and Path fields.
struct FieldClassification {
    /// Fields typed as `cog.File` (or `list[File]`, `Optional[File]`, etc.)
    /// These use `File.validate()` for URL coercion.
    file_fields: HashSet<String>,
    /// Fields typed as `cog.Path` (or `list[Path]`, `Optional[Path]`, etc.)
    /// These use `Path.validate()` for URL coercion.
    path_fields: HashSet<String>,
}

/// Inspect a Python function's type annotations to find parameters typed as
/// `cog.File` or `cog.Path` (including `list[...]`, `Optional[...]`,
/// `... | None`, etc.).
///
/// Returns a `FieldClassification` so that `coerce_url_strings` only coerces
/// fields that are actually File- or Path-typed, leaving `str` and other types
/// untouched.
fn classify_fields(py: Python<'_>, func: &Bound<'_, PyAny>) -> PyResult<FieldClassification> {
    let mut file_fields = HashSet::new();
    let mut path_fields = HashSet::new();

    let cog_types = py.import("cog.types")?;
    let cog_file_class = cog_types.getattr("File")?;
    let cog_path_class = cog_types.getattr("Path")?;

    // typing.get_type_hints resolves string annotations and handles forward refs
    let typing = py.import("typing")?;
    let get_type_hints = typing.getattr("get_type_hints")?;
    let get_origin = typing.getattr("get_origin")?;
    let get_args = typing.getattr("get_args")?;
    let builtins_list = py.eval(c"list", None, None)?;
    let union_type = typing.getattr("Union")?;

    let hints = match get_type_hints.call1((func,)) {
        Ok(h) => h,
        Err(_) => {
            return Ok(FieldClassification {
                file_fields,
                path_fields,
            });
        }
    };

    // Helper: returns Some("file") if ty is File or list[File],
    // Some("path") if ty is cog.Path or list[cog.Path], None otherwise.
    let classify_type = |ty: &Bound<'_, PyAny>| -> PyResult<Option<&'static str>> {
        if ty.is(&cog_file_class) {
            return Ok(Some("file"));
        }
        if ty.is(&cog_path_class) {
            return Ok(Some("path"));
        }
        let inner_origin = get_origin.call1((ty,))?;
        if !inner_origin.is_none() && inner_origin.is(&builtins_list) {
            let inner_args = get_args.call1((ty,))?;
            if let Ok(t) = inner_args.cast::<pyo3::types::PyTuple>()
                && !t.is_empty()
            {
                let inner = t.get_item(0)?;
                if inner.is(&cog_file_class) {
                    return Ok(Some("file"));
                }
                if inner.is(&cog_path_class) {
                    return Ok(Some("path"));
                }
            }
        }
        Ok(None)
    };

    let hints_dict = hints.cast::<PyDict>()?;
    for (name, annotation) in hints_dict.iter() {
        let name_str: String = match name.extract() {
            Ok(s) => s,
            Err(_) => continue,
        };
        if name_str == "return" {
            continue;
        }

        // Direct annotation: `param: File` or `param: Path`
        // Also covers `list[File]` / `list[Path]` via classify_type.
        if let Some(kind) = classify_type(&annotation)? {
            match kind {
                "file" => {
                    file_fields.insert(name_str);
                }
                "path" => {
                    path_fields.insert(name_str);
                }
                _ => {}
            }
            continue;
        }

        // Union annotation: Optional[File], File | None, Optional[list[File]], etc.
        // typing.get_origin(Optional[X]) -> typing.Union
        // typing.get_args(Optional[X])   -> (X, NoneType)
        let origin = get_origin.call1((&annotation,))?;
        if !origin.is_none() && origin.is(&union_type) {
            let args = get_args.call1((&annotation,))?;
            if let Ok(args_tuple) = args.cast::<pyo3::types::PyTuple>() {
                for arg in args_tuple.iter() {
                    // Skip NoneType
                    if arg.is_none() || arg.is(py.None().into_bound(py)) {
                        continue;
                    }
                    let nonetype = py.eval(c"type(None)", None, None)?;
                    if arg.is(&nonetype) {
                        continue;
                    }
                    if let Some(kind) = classify_type(&arg)? {
                        match kind {
                            "file" => {
                                file_fields.insert(name_str.clone());
                            }
                            "path" => {
                                path_fields.insert(name_str.clone());
                            }
                            _ => {}
                        }
                        break;
                    }
                }
            }
        }
    }

    if !file_fields.is_empty() || !path_fields.is_empty() {
        tracing::debug!(
            "Detected File-typed fields: {:?}, Path-typed fields: {:?}",
            file_fields,
            path_fields
        );
    }

    Ok(FieldClassification {
        file_fields,
        path_fields,
    })
}

/// Coerce URL string values in the input dict to the appropriate cog types.
///
/// After `json.loads()`, all values are plain Python types. URL strings
/// (http://, https://, data:) that represent file inputs need to be converted:
///   - `File`-typed fields -> `File.validate()` -> returns IO-like `URLFile`
///   - `Path`-typed fields -> `Path.validate()` -> returns `URLPath` (downloaded later)
///
/// Only fields whose declared type is `File` or `Path` (including `list[...]`,
/// `Optional[...]`, etc.) are coerced. Fields typed as `str` or any other type
/// are left untouched, even if the value looks like a URL.
///
/// This replaces the type coercion that `_adt.py`'s `PrimitiveType.normalize()`
/// previously performed.
fn coerce_url_strings(
    py: Python<'_>,
    payload: &Bound<'_, PyDict>,
    fields: &FieldClassification,
) -> PyResult<()> {
    let cog_types = py.import("cog.types")?;
    let path_validate = cog_types.getattr("Path")?.getattr("validate")?;
    let file_validate = cog_types.getattr("File")?.getattr("validate")?;

    for (key, value) in payload.iter() {
        let key_str: String = key.extract().unwrap_or_default();

        // Only coerce fields that are declared as File or Path types.
        // str-typed and other fields are left as-is even if values look like URLs.
        let validate = if fields.file_fields.contains(&key_str) {
            &file_validate
        } else if fields.path_fields.contains(&key_str) {
            &path_validate
        } else {
            continue;
        };

        // Single string value -- check if it's a URL
        if let Ok(s) = value.extract::<String>() {
            if s.starts_with("http://") || s.starts_with("https://") || s.starts_with("data:") {
                let coerced = validate.call1((&value,))?;
                payload.set_item(&key, coerced)?;
            }
        }
        // List of strings -- check if any are URLs
        else if let Ok(list) = value.extract::<Bound<'_, pyo3::types::PyList>>() {
            let mut any_coerced = false;
            let new_items = pyo3::types::PyList::empty(py);
            for item in list.iter() {
                if let Ok(s) = item.extract::<String>()
                    && (s.starts_with("http://")
                        || s.starts_with("https://")
                        || s.starts_with("data:"))
                {
                    let coerced = validate.call1((&item,))?;
                    new_items.append(coerced)?;
                    any_coerced = true;
                    continue;
                }
                new_items.append(item)?;
            }
            if any_coerced {
                payload.set_item(&key, new_items)?;
            }
        }
    }
    Ok(())
}

/// Download URLPath inputs in parallel and replace them in the payload dict.
///
/// This replicates the behavior from cog's worker.py:
/// - Find all URLPath instances in the payload dict
/// - Download them in parallel using ThreadPoolExecutor
/// - Replace URLPath values with local Path in the dict
///
/// Returns the downloaded Path objects for cleanup on drop.
fn download_url_paths_into_dict(
    py: Python<'_>,
    payload: &Bound<'_, PyDict>,
) -> PyResult<Vec<PyObject>> {
    let cog_types = py.import("cog.types")?;
    let url_path_class = cog_types.getattr("URLPath")?;

    // Collect URLPath fields that need downloading
    // Structure: (key, value, is_list)
    let mut url_path_keys: Vec<(String, bool)> = Vec::new();

    for (key, value) in payload.iter() {
        let key_str: String = key.extract()?;

        if value.is_instance(&url_path_class)? {
            url_path_keys.push((key_str, false));
        }
        // Check for lists of URLPath
        else if let Ok(list) = value.extract::<Bound<'_, pyo3::types::PyList>>()
            && !list.is_empty()
        {
            let all_url_paths = list
                .iter()
                .all(|item| item.is_instance(&url_path_class).unwrap_or(false));
            if all_url_paths {
                url_path_keys.push((key_str, true));
            }
        }
    }

    if url_path_keys.is_empty() {
        return Ok(Vec::new());
    }

    tracing::debug!("Downloading {} URLPath input(s)", url_path_keys.len());

    // Use ThreadPoolExecutor to download in parallel (like worker.py)
    let concurrent_futures = py.import("concurrent.futures")?;
    let executor_class = concurrent_futures.getattr("ThreadPoolExecutor")?;
    let executor = executor_class.call1((8,))?; // max_workers=8

    // Structure to track futures: (key, future_or_futures, is_list)
    let mut futs: std::collections::HashMap<String, (Vec<Bound<'_, PyAny>>, bool)> =
        std::collections::HashMap::new();
    let mut all_futures: Vec<Bound<'_, PyAny>> = Vec::new();

    for (key, is_list) in &url_path_keys {
        let value = payload.get_item(key)?.ok_or_else(|| {
            pyo3::exceptions::PyKeyError::new_err(format!(
                "Input key '{}' disappeared during processing",
                key
            ))
        })?;

        if *is_list {
            let list = value.extract::<Bound<'_, pyo3::types::PyList>>()?;
            let mut futures_for_key = Vec::new();
            for item in list.iter() {
                let convert_method = item.getattr("convert")?;
                let future = executor.call_method1("submit", (convert_method,))?;
                futures_for_key.push(future.clone());
                all_futures.push(future);
            }
            futs.insert(key.clone(), (futures_for_key, true));
        } else {
            let convert_method = value.getattr("convert")?;
            let future = executor.call_method1("submit", (convert_method,))?;
            all_futures.push(future.clone());
            futs.insert(key.clone(), (vec![future], false));
        }
    }

    // Wait for all futures
    let future_list = pyo3::types::PyList::new(py, &all_futures)?;
    let wait_fn = concurrent_futures.getattr("wait")?;
    let wait_result = wait_fn.call1((&future_list,))?;
    let done = wait_result.get_item(0)?;
    let not_done = wait_result.get_item(1)?;

    // Check for failures
    let not_done_len: usize = not_done.len()?;
    if not_done_len > 0 {
        // Cancel remaining and find the exception
        for item in not_done.try_iter()? {
            let fut = item?;
            let _ = fut.call_method0("cancel");
        }
        // Find and raise the exception
        for item in done.try_iter()? {
            let fut = item?;
            fut.call_method0("result")?; // raises if future finished with exception
        }
        return Err(PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(
            "Download failed",
        ));
    }

    // All downloads complete - replace URLPath with local Path in payload
    // Collect the Path objects for cleanup
    let mut cleanup_paths: Vec<PyObject> = Vec::new();

    for (key, (futures, is_list)) in futs {
        if is_list {
            let mut results = Vec::new();
            for fut in futures {
                let result = fut.call_method0("result")?;
                cleanup_paths.push(result.clone().unbind());
                results.push(result);
            }
            let result_list = pyo3::types::PyList::new(py, &results)?;
            payload.set_item(&key, result_list)?;
        } else {
            let result = futures[0].call_method0("result")?;
            cleanup_paths.push(result.clone().unbind());
            payload.set_item(&key, result)?;
        }
    }

    // Shutdown executor
    executor.call_method0("shutdown")?;

    tracing::debug!(
        "URLPath downloads complete, {} paths to cleanup",
        cleanup_paths.len()
    );
    Ok(cleanup_paths)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Helper: define a Python function with the given parameter annotations and
    /// return the `FieldClassification` from `classify_fields`.
    fn classify_for(py_func_src: &str) -> FieldClassification {
        pyo3::Python::initialize();
        Python::attach(|py| {
            // Ensure cog.types is importable
            py.run(c"import cog.types", None, None)
                .expect("cog.types must be importable for tests");

            let locals = PyDict::new(py);
            py.run(
                &std::ffi::CString::new(py_func_src).unwrap(),
                None,
                Some(&locals),
            )
            .expect("failed to define test function");

            let func = locals.get_item("func").unwrap().unwrap();
            classify_fields(py, &func).expect("classify_fields failed")
        })
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_direct_file() {
        let c = classify_for("from cog import File\ndef func(a: File, b: str): ...");
        assert!(
            c.file_fields.contains("a"),
            "direct File annotation not detected"
        );
        assert!(
            !c.file_fields.contains("b"),
            "str incorrectly flagged as File"
        );
        assert!(
            !c.path_fields.contains("b"),
            "str incorrectly flagged as Path"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_list_file() {
        let c = classify_for("from cog import File\ndef func(a: list[File]): ...");
        assert!(
            c.file_fields.contains("a"),
            "list[File] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_optional_file() {
        let c = classify_for(
            "from typing import Optional\nfrom cog import File\ndef func(a: Optional[File]): ...",
        );
        assert!(
            c.file_fields.contains("a"),
            "Optional[File] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_file_union_none() {
        let c = classify_for(
            "from typing import Union\nfrom cog import File\ndef func(a: Union[File, None]): ...",
        );
        assert!(
            c.file_fields.contains("a"),
            "File | None / Union[File, None] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_optional_list_file() {
        let c = classify_for(
            "from typing import Optional\nfrom cog import File\ndef func(a: Optional[list[File]]): ...",
        );
        assert!(
            c.file_fields.contains("a"),
            "Optional[list[File]] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_direct_path() {
        let c = classify_for("from cog import Path\ndef func(a: Path, b: str): ...");
        assert!(
            c.path_fields.contains("a"),
            "direct Path annotation not detected"
        );
        assert!(
            !c.path_fields.contains("b"),
            "str incorrectly flagged as Path"
        );
        assert!(
            !c.file_fields.contains("b"),
            "str incorrectly flagged as File"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_list_path() {
        let c = classify_for("from cog import Path\ndef func(a: list[Path]): ...");
        assert!(
            c.path_fields.contains("a"),
            "list[Path] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_optional_path() {
        let c = classify_for(
            "from typing import Optional\nfrom cog import Path\ndef func(a: Optional[Path]): ...",
        );
        assert!(
            c.path_fields.contains("a"),
            "Optional[Path] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn non_file_or_path_types_not_detected() {
        let c = classify_for(
            "from typing import Optional\ndef func(a: str, b: int, c: Optional[str]): ...",
        );
        assert!(
            c.file_fields.is_empty(),
            "non-File types incorrectly detected as file: {:?}",
            c.file_fields
        );
        assert!(
            c.path_fields.is_empty(),
            "non-Path types incorrectly detected as path: {:?}",
            c.path_fields
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn str_url_input_not_coerced() {
        let c = classify_for(
            "from cog import File, Path\ndef func(a: str, b: File, c: Path, d: list[str]): ...",
        );
        assert!(
            !c.file_fields.contains("a") && !c.path_fields.contains("a"),
            "str field 'a' should not be classified as File or Path"
        );
        assert!(
            c.file_fields.contains("b"),
            "File field 'b' should be classified"
        );
        assert!(
            c.path_fields.contains("c"),
            "Path field 'c' should be classified"
        );
        assert!(
            !c.file_fields.contains("d") && !c.path_fields.contains("d"),
            "list[str] field 'd' should not be classified as File or Path"
        );
    }
}
