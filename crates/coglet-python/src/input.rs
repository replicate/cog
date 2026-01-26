//! Input processing for different predictor runtimes.
//!
//! Cog predictors can come from different runtimes with different type systems:
//! - **Pydantic (cog)**: Uses Pydantic BaseModel for input validation, URLPath for file downloads
//! - **Non-pydantic (cog-dataclass)**: Uses ADT types, different file handling
//!
//! This module provides a trait-based abstraction to handle input processing
//! for each runtime correctly.

use std::path::Path;

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

/// Detected predictor runtime.
#[derive(Debug)]
pub enum Runtime {
    /// Pydantic-based cog runtime.
    /// Uses BaseInput for validation, URLPath for file inputs.
    Pydantic {
        /// The Pydantic input model class (BaseInput subclass)
        input_type: PyObject,
    },
    /// Non-pydantic runtime (cog-dataclass).
    /// Uses cog-dataclass ADT types.
    NonPydantic {
        /// The adt.PredictorInfo object with inputs dict
        adt_predictor: PyObject,
    },
}

/// Input processor trait for runtime-specific input handling.
pub trait InputProcessor: Send + Sync {
    /// Prepare input for prediction.
    ///
    /// This performs:
    /// 1. Validation (Pydantic or ADT)
    /// 2. Type coercion
    /// 3. File downloads (for Pydantic URLPath inputs)
    ///
    /// Returns a PreparedInput that cleans up temp files on drop.
    fn prepare(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PreparedInput>;
}

/// Pydantic input processor for cog runtime.
pub struct PydanticInputProcessor {
    /// The Pydantic input model class (BaseInput subclass)
    input_type: PyObject,
}

impl PydanticInputProcessor {
    pub fn new(input_type: PyObject) -> Self {
        Self { input_type }
    }
}

impl InputProcessor for PydanticInputProcessor {
    fn prepare(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PreparedInput> {
        // 1. Validate input through Pydantic model
        //    InputType(**input_dict) creates URLPath objects for cog.Path fields
        let input_type = self.input_type.bind(py);
        let validated = input_type.call((), Some(input))?;

        // 2. Get dict from validated model (preserves URLPath objects)
        let cog_types = py.import("cog.types")?;
        let pydantic_v2: bool = cog_types.getattr("PYDANTIC_V2")?.extract()?;

        let payload = if pydantic_v2 {
            // Pydantic v2: model_dump(), then unwrap serialization iterators
            let dumped = validated.call_method0("model_dump")?;
            let helpers = py.import("cog.server.helpers")?;
            let unwrap = helpers.getattr("unwrap_pydantic_serialization_iterators")?;
            unwrap.call1((&dumped,))?
        } else {
            // Pydantic v1: dict()
            validated.call_method0("dict")?
        };

        #[allow(deprecated)]
        let payload_dict = payload.downcast::<PyDict>()?;

        // 3. Download URLPath inputs in parallel and replace in payload
        //    This mutates payload_dict and returns the downloaded paths for cleanup
        let cleanup_paths = download_url_paths_into_dict(py, payload_dict)?;

        Ok(PreparedInput::new(
            payload_dict.clone().unbind(),
            cleanup_paths,
        ))
    }
}

/// Input processor for non-pydantic runtime (cog-dataclass).
pub struct NonPydanticInputProcessor {
    /// The adt.PredictorInfo object
    adt_predictor: PyObject,
}

impl NonPydanticInputProcessor {
    pub fn new(adt_predictor: PyObject) -> Self {
        Self { adt_predictor }
    }
}

impl InputProcessor for NonPydanticInputProcessor {
    fn prepare(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PreparedInput> {
        // Use cog-dataclass inspector.check_input() for validation
        let inspector = py.import("cog._inspector")?;
        let check_input = inspector.getattr("check_input")?;

        // Get inputs dict from adt_predictor
        let adt_predictor = self.adt_predictor.bind(py);
        let adt_inputs = adt_predictor.getattr("inputs")?;

        // check_input(adt_inputs, input_dict) -> validated kwargs dict
        let result = check_input.call1((&adt_inputs, input))?;
        let result_dict = result.extract::<Bound<'_, PyDict>>()?;

        // Download URLPath inputs in parallel and replace in payload
        let cleanup_paths = download_url_paths_into_dict(py, &result_dict)?;

        Ok(PreparedInput::new(result_dict.unbind(), cleanup_paths))
    }
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

/// Error returned when runtime detection fails.
#[derive(Debug)]
pub struct RuntimeDetectionError {
    pub predictor_ref: String,
}

impl std::fmt::Display for RuntimeDetectionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "Unable to detect predictor runtime for '{}'. \
             Expected either Pydantic (cog) or non-pydantic (cog-dataclass) type system. \
             This predictor may be incompatible with the runtime.",
            self.predictor_ref
        )
    }
}

impl std::error::Error for RuntimeDetectionError {}

/// Detect the runtime type for a loaded predictor.
///
/// Returns the appropriate Runtime variant based on the predictor's type system.
/// Returns an error if the runtime cannot be determined.
pub fn detect_runtime(
    py: Python<'_>,
    predictor_ref: &str,
    instance: &PyObject,
) -> Result<Runtime, RuntimeDetectionError> {
    // Try Pydantic first
    if let Some(runtime) = try_pydantic_runtime(py, instance) {
        tracing::info!("Detected Pydantic runtime");
        return Ok(runtime);
    }

    // Try non-pydantic runtime (cog-dataclass)
    if let Some(runtime) = try_non_pydantic_runtime(py, predictor_ref) {
        tracing::info!("Detected non-pydantic runtime");
        return Ok(runtime);
    }

    // Cannot determine runtime
    Err(RuntimeDetectionError {
        predictor_ref: predictor_ref.to_string(),
    })
}

/// Try to detect Pydantic runtime.
fn try_pydantic_runtime(py: Python<'_>, instance: &PyObject) -> Option<Runtime> {
    let cog_predictor = py.import("cog.predictor").ok()?;
    let get_input_type = cog_predictor.getattr("get_input_type").ok()?;

    // get_input_type returns the Pydantic input model class
    let input_type = get_input_type.call1((instance.bind(py),)).ok()?;

    // Verify it's a BaseInput subclass using issubclass()
    let base_input = py.import("cog.base_input").ok()?;
    let base_input_class = base_input.getattr("BaseInput").ok()?;

    let builtins = py.import("builtins").ok()?;
    let issubclass = builtins.getattr("issubclass").ok()?;
    let is_subclass: bool = issubclass
        .call1((&input_type, &base_input_class))
        .ok()
        .and_then(|r| r.extract().ok())
        .unwrap_or(false);

    if is_subclass {
        Some(Runtime::Pydantic {
            input_type: input_type.unbind(),
        })
    } else {
        None
    }
}

fn parse_predictor_ref(predictor_ref: &str) -> Option<(String, String)> {
    let parts: Vec<&str> = predictor_ref.rsplitn(2, ':').collect();
    if parts.len() != 2 {
        return None;
    }
    let predictor_name = parts[0].to_string();
    let module_path = parts[1];

    let module_name = if module_path.contains('/')
        || module_path.contains('\\')
        || module_path.ends_with(".py")
    {
        let normalized_path = module_path.replace('\\', "/");
        Path::new(&normalized_path)
            .file_stem()
            .and_then(|stem| stem.to_str())
            .unwrap_or(module_path)
            .to_string()
    } else {
        module_path.to_string()
    };

    Some((module_name, predictor_name))
}

/// Try to detect non-pydantic runtime (cog-dataclass).
fn try_non_pydantic_runtime(py: Python<'_>, predictor_ref: &str) -> Option<Runtime> {
    let (module_name, predictor_name) = parse_predictor_ref(predictor_ref)?;

    let inspector = py.import("cog._inspector").ok()?;
    let create_predictor = inspector.getattr("create_predictor").ok()?;

    let adt_predictor = create_predictor.call1((module_name, predictor_name)).ok()?;

    let adt_module = py.import("cog._adt").ok()?;
    let predictor_info_class = adt_module.getattr("PredictorInfo").ok()?;
    let is_predictor_info = adt_predictor
        .is_instance(&predictor_info_class)
        .ok()
        .unwrap_or(false);

    if !is_predictor_info {
        return None;
    }

    Some(Runtime::NonPydantic {
        adt_predictor: adt_predictor.unbind(),
    })
}

/// Create an InputProcessor for the given runtime.
pub fn create_input_processor(runtime: &Runtime) -> Box<dyn InputProcessor> {
    match runtime {
        Runtime::Pydantic { input_type } => {
            // Clone PyObject using Python GIL
            Python::attach(|py| Box::new(PydanticInputProcessor::new(input_type.clone_ref(py))))
        }
        Runtime::NonPydantic { adt_predictor } => Python::attach(|py| {
            Box::new(NonPydanticInputProcessor::new(adt_predictor.clone_ref(py)))
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_predictor_ref_valid() {
        let (module, predictor) = parse_predictor_ref("predict.py:Predictor").unwrap();
        assert_eq!(module, "predict");
        assert_eq!(predictor, "Predictor");
    }

    #[test]
    fn test_parse_predictor_ref_nested_path() {
        let (module, predictor) = parse_predictor_ref("path/to/predict.py:Predictor").unwrap();
        assert_eq!(module, "predict");
        assert_eq!(predictor, "Predictor");
    }

    #[test]
    fn test_parse_predictor_ref_function() {
        let (module, predictor) = parse_predictor_ref("predict.py:predict").unwrap();
        assert_eq!(module, "predict");
        assert_eq!(predictor, "predict");
    }

    #[test]
    fn test_parse_predictor_ref_non_standard_name() {
        let (module, predictor) = parse_predictor_ref("model.py:run").unwrap();
        assert_eq!(module, "model");
        assert_eq!(predictor, "run");
    }

    #[test]
    fn test_parse_predictor_ref_windows_path() {
        let (module, predictor) = parse_predictor_ref("path\\to\\predict.py:Predictor").unwrap();
        assert_eq!(module, "predict");
        assert_eq!(predictor, "Predictor");
    }

    #[test]
    fn test_parse_predictor_ref_absolute_path() {
        let (module, predictor) = parse_predictor_ref("/tmp/scratch/predict.py:Predictor").unwrap();
        assert_eq!(module, "predict");
        assert_eq!(predictor, "Predictor");
    }

    #[test]
    fn test_parse_predictor_ref_invalid_no_colon() {
        assert!(parse_predictor_ref("predict.py").is_none());
    }

    #[test]
    fn test_parse_predictor_ref_invalid_multiple_colons() {
        // Should take the last colon as the separator
        let (module, predictor) = parse_predictor_ref("path:to:predict.py:Predictor").unwrap();
        assert_eq!(module, "path:to:predict");
        assert_eq!(predictor, "Predictor");
    }
}
