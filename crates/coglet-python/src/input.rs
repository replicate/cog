//! Input processing for cog predictors.
//!
//! This module handles file downloads for cog predictor inputs.
//! Input validation is performed at the HTTP edge using the OpenAPI schema;
//! the worker only needs to download URLPath inputs and pass them through.

use std::collections::HashMap;

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

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum CoercionTarget {
    File,
    Path,
}

#[derive(Debug)]
enum CoercionPlan {
    /// Type hints resolved; only explicitly typed fields are coerced.
    Explicit(HashMap<String, CoercionTarget>),
    /// Type hints failed to resolve; preserve legacy fallback behavior by
    /// coercing URL-like strings via Path.validate().
    FallbackAllPath,
}

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
    let coercion_plan = detect_coercion_plan(py, func)?;
    coerce_url_strings(py, input, &coercion_plan)?;
    let cleanup_paths = download_url_paths_into_dict(py, input)?;
    Ok(PreparedInput::new(input.clone().unbind(), cleanup_paths))
}

/// Inspect a Python function's type annotations and map coercion targets for
/// fields explicitly annotated as `cog.File` or `cog.Path` (including list and
/// Optional/Union wrappers).
fn detect_coercion_plan(py: Python<'_>, func: &Bound<'_, PyAny>) -> PyResult<CoercionPlan> {
    let mut targets = HashMap::new();

    let cog_types = py.import("cog.types")?;
    let cog_file_class = cog_types.getattr("File")?;
    let cog_path_class = cog_types.getattr("Path")?;

    // typing.get_type_hints resolves string annotations and handles forward refs
    let typing = py.import("typing")?;
    let get_type_hints = typing.getattr("get_type_hints")?;
    let get_origin = typing.getattr("get_origin")?;
    let get_args = typing.getattr("get_args")?;
    let types_module = py.import("types")?;
    let builtins_list = py.eval(c"list", None, None)?;
    let typing_union_type = typing.getattr("Union")?;
    let pep604_union_type = types_module.getattr("UnionType")?;
    let nonetype = py.eval(c"type(None)", None, None)?;

    let hints = match get_type_hints.call1((func,)) {
        Ok(h) => h,
        Err(err) => {
            tracing::debug!(
                error = %err,
                "Failed to resolve type hints; falling back to legacy Path coercion"
            );
            return Ok(CoercionPlan::FallbackAllPath);
        }
    };

    let target_for_annotation = |ty: &Bound<'_, PyAny>| -> PyResult<Option<CoercionTarget>> {
        if ty.is(&cog_file_class) {
            return Ok(Some(CoercionTarget::File));
        }
        if ty.is(&cog_path_class) {
            return Ok(Some(CoercionTarget::Path));
        }

        let origin = get_origin.call1((ty,))?;
        if !origin.is_none() && origin.is(&builtins_list) {
            let args = get_args.call1((ty,))?;
            if let Ok(t) = args.cast::<pyo3::types::PyTuple>()
                && !t.is_empty()
            {
                let inner = t.get_item(0)?;
                if inner.is(&cog_file_class) {
                    return Ok(Some(CoercionTarget::File));
                }
                if inner.is(&cog_path_class) {
                    return Ok(Some(CoercionTarget::Path));
                }
            }
            return Ok(None);
        }

        let is_union =
            !origin.is_none() && (origin.is(&typing_union_type) || origin.is(&pep604_union_type));
        if is_union {
            let args = get_args.call1((ty,))?;
            if let Ok(args_tuple) = args.cast::<pyo3::types::PyTuple>() {
                let mut union_target: Option<CoercionTarget> = None;
                for arg in args_tuple.iter() {
                    if arg.is(&nonetype) {
                        continue;
                    }
                    let arg_target = if arg.is(&cog_file_class) {
                        Some(CoercionTarget::File)
                    } else if arg.is(&cog_path_class) {
                        Some(CoercionTarget::Path)
                    } else {
                        let arg_origin = get_origin.call1((&arg,))?;
                        if !arg_origin.is_none() && arg_origin.is(&builtins_list) {
                            let inner_args = get_args.call1((&arg,))?;
                            if let Ok(inner) = inner_args.cast::<pyo3::types::PyTuple>() {
                                if !inner.is_empty() && inner.get_item(0)?.is(&cog_file_class) {
                                    Some(CoercionTarget::File)
                                } else if !inner.is_empty()
                                    && inner.get_item(0)?.is(&cog_path_class)
                                {
                                    Some(CoercionTarget::Path)
                                } else {
                                    None
                                }
                            } else {
                                None
                            }
                        } else {
                            None
                        }
                    };

                    match (union_target, arg_target) {
                        (None, Some(t)) => union_target = Some(t),
                        (Some(existing), Some(candidate)) if existing == candidate => {}
                        (Some(_), Some(_)) => return Ok(None),
                        _ => {}
                    }
                }
                return Ok(union_target);
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

        if let Some(target) = target_for_annotation(&annotation)? {
            targets.insert(name_str, target);
        }
    }

    if !targets.is_empty() {
        tracing::debug!("Detected coercion targets: {:?}", targets);
    }

    Ok(CoercionPlan::Explicit(targets))
}

/// Coerce URL string values in the input dict to the appropriate cog types.
///
/// After `json.loads()`, all values are plain Python types. URL strings
/// (http://, https://, data:) that represent file inputs need to be converted:
///   - `File`-typed fields -> `File.validate()` -> returns IO-like `URLFile`
///   - `Path`-typed fields -> `Path.validate()` -> returns `URLPath` (downloaded later)
///
/// This replaces the type coercion that `_adt.py`'s `PrimitiveType.normalize()`
/// previously performed.
fn coerce_url_strings(
    py: Python<'_>,
    payload: &Bound<'_, PyDict>,
    coercion_plan: &CoercionPlan,
) -> PyResult<()> {
    let cog_types = py.import("cog.types")?;
    let path_validate = cog_types.getattr("Path")?.getattr("validate")?;
    let file_validate = cog_types.getattr("File")?.getattr("validate")?;

    for (key, value) in payload.iter() {
        let validate = match coercion_plan {
            CoercionPlan::FallbackAllPath => &path_validate,
            CoercionPlan::Explicit(coercion_targets) => {
                let key_str: String = match key.extract() {
                    Ok(k) => k,
                    Err(_) => continue,
                };
                let Some(target) = coercion_targets.get(&key_str) else {
                    continue;
                };
                match target {
                    CoercionTarget::File => &file_validate,
                    CoercionTarget::Path => &path_validate,
                }
            }
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
    /// return the explicit target map from `detect_coercion_plan`.
    fn explicit_coercion_targets_for(py_func_src: &str) -> HashMap<String, CoercionTarget> {
        pyo3::Python::initialize();
        Python::attach(|py| {
            // Ensure cog.types.File is importable
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
            match detect_coercion_plan(py, &func).expect("detect_coercion_plan failed") {
                CoercionPlan::Explicit(targets) => targets,
                CoercionPlan::FallbackAllPath => {
                    panic!("unexpected fallback plan for explicit type test")
                }
            }
        })
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_direct_file() {
        let targets =
            explicit_coercion_targets_for("from cog import File\ndef func(a: File, b: str): ...");
        assert_eq!(
            targets.get("a"),
            Some(&CoercionTarget::File),
            "direct File annotation not detected"
        );
        assert!(
            !targets.contains_key("b"),
            "str should not be marked for coercion"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_list_file() {
        let targets =
            explicit_coercion_targets_for("from cog import File\ndef func(a: list[File]): ...");
        assert_eq!(
            targets.get("a"),
            Some(&CoercionTarget::File),
            "list[File] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_optional_file() {
        let targets = explicit_coercion_targets_for(
            "from typing import Optional\nfrom cog import File\ndef func(a: Optional[File]): ...",
        );
        assert!(
            targets.get("a") == Some(&CoercionTarget::File),
            "Optional[File] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_file_union_none() {
        let targets = explicit_coercion_targets_for(
            "from typing import Union\nfrom cog import File\ndef func(a: Union[File, None]): ...",
        );
        assert!(
            targets.get("a") == Some(&CoercionTarget::File),
            "File | None / Union[File, None] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_optional_list_file() {
        let targets = explicit_coercion_targets_for(
            "from typing import Optional\nfrom cog import File\ndef func(a: Optional[list[File]]): ...",
        );
        assert!(
            targets.get("a") == Some(&CoercionTarget::File),
            "Optional[list[File]] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_path_type() {
        let targets = explicit_coercion_targets_for("from cog import Path\ndef func(a: Path): ...");
        assert_eq!(
            targets.get("a"),
            Some(&CoercionTarget::Path),
            "Path annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn detect_optional_list_path() {
        let targets = explicit_coercion_targets_for(
            "from typing import Optional\nfrom cog import Path\ndef func(a: Optional[list[Path]]): ...",
        );
        assert_eq!(
            targets.get("a"),
            Some(&CoercionTarget::Path),
            "Optional[list[Path]] annotation not detected"
        );
    }

    #[test]
    #[ignore] // Requires cog Python package in PYTHONPATH
    fn non_target_types_not_detected() {
        let targets = explicit_coercion_targets_for(
            "from pathlib import Path\nfrom typing import Optional\ndef func(a: str, b: int, c: Optional[str], d: Path): ...",
        );
        assert!(
            targets.is_empty(),
            "non-cog File/Path types incorrectly detected: {:?}",
            targets
        );
    }
}
