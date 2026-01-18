//! Input processing for different predictor runtimes.
//!
//! Cog predictors can come from different runtimes with different type systems:
//! - **Pydantic (cog)**: Uses Pydantic BaseModel for input validation, URLPath for file downloads
//! - **Coglet**: Uses dataclasses and ADT types, different file handling
//!
//! This module provides a trait-based abstraction to handle input processing
//! for each runtime correctly.

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
    /// Coglet runtime (alpha/beta).
    /// Uses dataclasses and ADT types.
    Coglet {
        /// The adt.Predictor object with inputs dict
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
            // Pydantic v2: model_dump()
            validated.call_method0("model_dump")?
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

/// Coglet input processor for coglet alpha/beta runtimes.
pub struct CogletInputProcessor {
    /// The adt.Predictor object
    adt_predictor: PyObject,
}

impl CogletInputProcessor {
    pub fn new(adt_predictor: PyObject) -> Self {
        Self { adt_predictor }
    }
}

impl InputProcessor for CogletInputProcessor {
    fn prepare(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PreparedInput> {
        // Use coglet's inspector.check_input() for validation
        let inspector = py.import("coglet.inspector")?;
        let check_input = inspector.getattr("check_input")?;

        // Get inputs dict from adt_predictor
        let adt_predictor = self.adt_predictor.bind(py);
        let adt_inputs = adt_predictor.getattr("inputs")?;

        // check_input(adt_inputs, input_dict) -> validated kwargs dict
        let result = check_input.call1((&adt_inputs, input))?;
        let result_dict = result.extract::<Bound<'_, PyDict>>()?;

        // Coglet doesn't download URLs, so no cleanup needed
        Ok(PreparedInput::new(result_dict.unbind(), Vec::new()))
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
        let value = payload.get_item(key)?.unwrap();

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
             Expected either Pydantic (cog) or Coglet type system. \
             This predictor may be incompatible with coglet.",
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

    // Try Coglet
    if let Some(runtime) = try_coglet_runtime(py, predictor_ref) {
        tracing::info!("Detected Coglet runtime");
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

/// Try to detect Coglet runtime.
fn try_coglet_runtime(py: Python<'_>, predictor_ref: &str) -> Option<Runtime> {
    // Parse predictor_ref to get module and class name
    let parts: Vec<&str> = predictor_ref.rsplitn(2, ':').collect();
    if parts.len() != 2 {
        return None;
    }
    let predictor_name = parts[0];
    let module_path = parts[1];

    // Convert file path to module name
    let module_name = module_path
        .trim_end_matches(".py")
        .replace(['/', '\\'], ".");

    let inspector = py.import("coglet.inspector").ok()?;
    let create_predictor = inspector.getattr("create_predictor").ok()?;

    // create_predictor returns adt.Predictor
    // Pass inspect_ast=False to skip AST validation (for older models)
    let adt_predictor = create_predictor
        .call1((&module_name, predictor_name, false))
        .ok()?;

    Some(Runtime::Coglet {
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
        Runtime::Coglet { adt_predictor } => {
            Python::attach(|py| Box::new(CogletInputProcessor::new(adt_predictor.clone_ref(py))))
        }
    }
}
