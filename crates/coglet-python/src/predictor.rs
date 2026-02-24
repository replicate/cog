//! Python predictor loading and invocation.

use std::sync::{Arc, OnceLock};

use pyo3::prelude::*;
use pyo3::types::PyDict;

use coglet_core::worker::SlotSender;
use coglet_core::{PredictionError, PredictionOutput, PredictionResult};

use crate::cancel;
use crate::input::{self, InputProcessor, PreparedInput, Runtime};
use crate::output;

// =============================================================================
// Async helper functions — defined as Python strings, initialized once.
//
// These must be Python `async def` functions to participate in asyncio's event
// loop. They cannot be expressed as pure Rust because they use Python's async
// iteration protocol and ContextVar.set() before awaiting a coroutine.
// =============================================================================

/// Collects an async generator into a list. Initialized once, reused per-call.
static COLLECT_ASYNC_GEN: OnceLock<Py<PyAny>> = OnceLock::new();

/// Sets a ContextVar then awaits a coroutine. Initialized once, reused per-call.
static CTX_WRAPPER: OnceLock<Py<PyAny>> = OnceLock::new();

/// Get or initialize the `_collect_async_gen` Python helper.
fn get_collect_async_gen(py: Python<'_>) -> Result<Py<PyAny>, PredictionError> {
    if let Some(f) = COLLECT_ASYNC_GEN.get() {
        return Ok(f.clone_ref(py));
    }

    let code = c"\
async def _collect_async_gen(agen):
    results = []
    async for item in agen:
        results.append(item)
    return results
";
    let globals = PyDict::new(py);
    py.run(code, Some(&globals), None).map_err(|e| {
        PredictionError::Failed(format!("Failed to define _collect_async_gen: {e}"))
    })?;
    let f = globals
        .get_item("_collect_async_gen")
        .map_err(|e| PredictionError::Failed(format!("Failed to get _collect_async_gen: {e}")))?
        .ok_or_else(|| PredictionError::Failed("_collect_async_gen not found".to_string()))?
        .unbind();
    let _ = COLLECT_ASYNC_GEN.set(f.clone_ref(py));
    Ok(f)
}

/// Get or initialize the `_ctx_wrapper` Python helper.
fn get_ctx_wrapper(py: Python<'_>) -> Result<Py<PyAny>, PredictionError> {
    if let Some(f) = CTX_WRAPPER.get() {
        return Ok(f.clone_ref(py));
    }

    let code = c"\
async def _ctx_wrapper(coro, prediction_id, contextvar):
    contextvar.set(prediction_id)
    return await coro
";
    let globals = PyDict::new(py);
    py.run(code, Some(&globals), None)
        .map_err(|e| PredictionError::Failed(format!("Failed to define _ctx_wrapper: {e}")))?;
    let f = globals
        .get_item("_ctx_wrapper")
        .map_err(|e| PredictionError::Failed(format!("Failed to get _ctx_wrapper: {e}")))?
        .ok_or_else(|| PredictionError::Failed("_ctx_wrapper not found".to_string()))?
        .unbind();
    let _ = CTX_WRAPPER.set(f.clone_ref(py));
    Ok(f)
}

/// Check if a PyErr is a CancelationException or asyncio.CancelledError.
fn is_cancelation_exception(py: Python<'_>, err: &PyErr) -> bool {
    // Check for cog.server.exceptions.CancelationException
    if let Ok(exceptions) = py.import("cog.server.exceptions")
        && let Ok(cancel_exc) = exceptions.getattr("CancelationException")
        && err.is_instance(py, &cancel_exc)
    {
        return true;
    }

    // Check for asyncio.CancelledError
    if let Ok(asyncio) = py.import("asyncio")
        && let Ok(cancelled_error) = asyncio.getattr("CancelledError")
        && err.is_instance(py, &cancelled_error)
    {
        return true;
    }

    false
}

/// Format a Python validation error.
///
/// Cog validation errors are already formatted as "field: message".
fn format_validation_error(py: Python<'_>, err: &PyErr) -> String {
    err.value(py).to_string()
}

/// Send a single output item over IPC, routing file outputs to disk.
///
/// For Path outputs (os.PathLike): sends the existing file path via send_file_output.
/// For IOBase outputs: reads bytes, writes to output_dir via write_file_output.
/// For everything else: processes through make_encodeable + upload_files, then send_output.
fn send_output_item(
    py: Python<'_>,
    item: &Bound<'_, PyAny>,
    json_module: &Bound<'_, PyAny>,
    slot_sender: &SlotSender,
) -> Result<(), PredictionError> {
    let os = py
        .import("os")
        .map_err(|e| PredictionError::Failed(format!("Failed to import os: {}", e)))?;
    let io_mod = py
        .import("io")
        .map_err(|e| PredictionError::Failed(format!("Failed to import io: {}", e)))?;
    let pathlike = os
        .getattr("PathLike")
        .map_err(|e| PredictionError::Failed(format!("Failed to get os.PathLike: {}", e)))?;
    let iobase = io_mod
        .getattr("IOBase")
        .map_err(|e| PredictionError::Failed(format!("Failed to get io.IOBase: {}", e)))?;

    if item.is_instance(&pathlike).unwrap_or(false) {
        // Path output — file already on disk, send path reference
        let path_str: String = item
            .call_method0("__fspath__")
            .and_then(|p| p.extract())
            .map_err(|e| PredictionError::Failed(format!("Failed to get fspath: {}", e)))?;
        slot_sender
            .send_file_output(std::path::PathBuf::from(path_str), None)
            .map_err(|e| PredictionError::Failed(format!("Failed to send file output: {}", e)))?;
        return Ok(());
    }

    if item.is_instance(&iobase).unwrap_or(false) {
        // IOBase output — read bytes, write to disk via SlotSender
        // Seek to start if seekable
        if item
            .call_method0("seekable")
            .and_then(|r| r.extract::<bool>())
            .unwrap_or(false)
        {
            let _ = item.call_method1("seek", (0,));
        }
        let data: Vec<u8> = item
            .call_method0("read")
            .and_then(|d| d.extract())
            .map_err(|e| PredictionError::Failed(format!("Failed to read IOBase: {}", e)))?;

        // Try to guess extension from filename
        let ext = item
            .getattr("name")
            .and_then(|n| n.extract::<String>())
            .ok()
            .and_then(|name| {
                std::path::Path::new(&name)
                    .extension()
                    .and_then(|e| e.to_str())
                    .map(|s| s.to_string())
            })
            .unwrap_or_else(|| "bin".to_string());

        slot_sender
            .write_file_output(&data, &ext, None)
            .map_err(|e| PredictionError::Failed(format!("Failed to write file output: {}", e)))?;
        return Ok(());
    }

    // Non-file output — process normally
    let processed = output::process_output_item(py, item)
        .map_err(|e| PredictionError::Failed(format!("Failed to process output item: {}", e)))?;

    let item_str: String = json_module
        .call_method1("dumps", (&processed,))
        .map_err(|e| PredictionError::Failed(format!("Failed to serialize output item: {}", e)))?
        .extract()
        .map_err(|e| PredictionError::Failed(format!("Failed to extract output string: {}", e)))?;

    let item_json: serde_json::Value = serde_json::from_str(&item_str)
        .map_err(|e| PredictionError::Failed(format!("Failed to parse output JSON: {}", e)))?;

    slot_sender
        .send_output(item_json)
        .map_err(|e| PredictionError::Failed(format!("Failed to send output: {}", e)))?;

    Ok(())
}

/// Type alias for Python object (Py<PyAny>).
type PyObject = Py<PyAny>;

/// How a predict() method executes
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PredictKind {
    /// Synchronous function: def predict(self, **input) -> Output
    Sync,
    /// Async coroutine: async def predict(self, **input) -> Output
    Async,
    /// Async generator: async def predict(self, **input) -> AsyncIterator[Output]
    AsyncGen,
}

/// Whether and how train() exists
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TrainKind {
    /// No train() method
    None,
    /// Synchronous: def train(self, **input) -> Output
    Sync,
    /// Async: async def train(self, **input) -> Output
    Async,
}

/// The predictor's structure and invocation target
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PredictorKind {
    /// Class instance with predict() method, optionally train()
    Class {
        predict: PredictKind,
        train: TrainKind,
    },
    /// Standalone function (e.g., train.py:train)
    /// The PredictKind describes how the function executes (sync/async/async_gen)
    StandaloneFunction(PredictKind),
}

/// A loaded Python predictor instance.
///
/// # GIL and Concurrency
///
/// This struct wraps a Python predictor object. The concurrency model depends on
/// the Python runtime:
///
/// ## GIL Python (default, 3.8-3.12, 3.13 default)
/// - `Python::attach()` acquires the GIL before calling into Python
/// - Only one thread can execute Python bytecode at a time
/// - However, native extensions (torch, numpy) release the GIL during compute
/// - CUDA operations in torch run without holding GIL, allowing I/O concurrency
/// - For sync predictors, max_concurrency=1 is appropriate
///
/// ## Free-threaded Python (3.13t+)
/// - No GIL, multiple threads can run Python simultaneously  
/// - `Python::attach()` still works but doesn't serialize execution
/// - Most ML models are NOT thread-safe (shared weights, CUDA contexts)
/// - Still need max_concurrency=1 for sync predictors unless model is thread-safe
///
/// ## Async Predictors
/// - `async def predict()` allows Python to manage concurrency
/// - Python's asyncio handles yielding during I/O
/// - Can support max_concurrency > 1 safely
///
/// # Runtime Detection
///
/// The predictor uses cog's ADT (Algebraic Data Type) system for input
/// validation and schema generation. The runtime is detected on load
/// and the appropriate input processor is used.
pub struct PythonPredictor {
    instance: PyObject,
    /// The predictor's kind (class or standalone function) and method execution types
    kind: PredictorKind,
    /// The detected runtime type (used during construction to select input processor).
    #[allow(dead_code)]
    runtime: Runtime,
    /// Input processor for this runtime.
    input_processor: Box<dyn InputProcessor>,
}

// PyObject is Send in PyO3 0.23+
// Safety: We only access the instance through Python::attach()
unsafe impl Send for PythonPredictor {}
unsafe impl Sync for PythonPredictor {}

impl PythonPredictor {
    /// Load a predictor from a reference like "predict.py:Predictor".
    pub fn load(py: Python<'_>, predictor_ref: &str) -> PyResult<Self> {
        // Import the cog.predictor module to use its loading function
        let cog_predictor = py.import("cog.predictor")?;
        let load_fn = cog_predictor.getattr("load_predictor_from_ref")?;

        // Load the predictor class and instantiate it
        let instance: PyObject = load_fn.call1((predictor_ref,))?.unbind();

        // Check if this is a standalone function (train mode) or a Predictor instance
        let inspect = py.import("inspect")?;
        let is_function: bool = inspect
            .call_method1("isfunction", (instance.bind(py),))?
            .extract()?;

        let kind = if is_function {
            // Standalone function - detect its async nature
            let (is_async, is_async_gen) = Self::detect_async(py, &instance, "")?;
            let predict_kind = if is_async_gen {
                tracing::info!("Detected async generator train()");
                PredictKind::AsyncGen
            } else if is_async {
                tracing::info!("Detected async train()");
                PredictKind::Async
            } else {
                tracing::info!("Detected sync train()");
                PredictKind::Sync
            };
            PredictorKind::StandaloneFunction(predict_kind)
        } else {
            // Class instance - detect predict() and train() methods
            let (is_async, is_async_gen) = Self::detect_async(py, &instance, "predict")?;
            let predict_kind = if is_async_gen {
                tracing::info!("Detected async generator predict()");
                PredictKind::AsyncGen
            } else if is_async {
                tracing::info!("Detected async predict()");
                PredictKind::Async
            } else {
                tracing::info!("Detected sync predict()");
                PredictKind::Sync
            };

            // Check if train() method exists and if it's async
            let train_kind = if instance.bind(py).hasattr("train")? {
                let (train_async, _) = Self::detect_async(py, &instance, "train")?;
                if train_async {
                    tracing::info!("Detected async train()");
                    TrainKind::Async
                } else {
                    tracing::info!("Detected sync train()");
                    TrainKind::Sync
                }
            } else {
                TrainKind::None
            };

            PredictorKind::Class {
                predict: predict_kind,
                train: train_kind,
            }
        };

        // Detect runtime and create input processor
        let runtime = input::detect_runtime(py, predictor_ref, &instance)
            .map_err(|e| pyo3::exceptions::PyRuntimeError::new_err(e.to_string()))?;
        let input_processor = input::create_input_processor(&runtime);

        Ok(Self {
            instance,
            kind,
            runtime,
            input_processor,
        })
    }

    /// Detect if a method is an async function.
    /// Returns (is_async, is_async_gen) tuple.
    ///
    /// If method_name is empty, checks the instance itself (for standalone functions).
    fn detect_async(
        py: Python<'_>,
        instance: &PyObject,
        method_name: &str,
    ) -> PyResult<(bool, bool)> {
        let inspect = py.import("inspect")?;

        // If method_name is empty, check the instance itself (standalone function)
        let target = if method_name.is_empty() {
            instance.bind(py).clone()
        } else {
            instance.bind(py).getattr(method_name)?
        };

        // Check isasyncgenfunction first (it's more specific)
        let is_async_gen: bool = inspect
            .call_method1("isasyncgenfunction", (&target,))?
            .extract()?;
        if is_async_gen {
            return Ok((true, true));
        }

        // Check iscoroutinefunction
        let is_coro: bool = inspect
            .call_method1("iscoroutinefunction", (&target,))?
            .extract()?;
        Ok((is_coro, false))
    }

    /// Returns true if this predictor has an async predict() method.
    pub fn is_async(&self) -> bool {
        match &self.kind {
            PredictorKind::Class { predict, .. } => {
                matches!(predict, PredictKind::Async | PredictKind::AsyncGen)
            }
            PredictorKind::StandaloneFunction(predict_kind) => {
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen)
            }
        }
    }

    /// Returns true if this predictor has a train() method.
    pub fn has_train(&self) -> bool {
        match &self.kind {
            PredictorKind::Class { train, .. } => !matches!(train, TrainKind::None),
            PredictorKind::StandaloneFunction(_) => true,
        }
    }

    /// Returns true if the train() method is async.
    pub fn is_train_async(&self) -> bool {
        match &self.kind {
            PredictorKind::Class { train, .. } => matches!(train, TrainKind::Async),
            PredictorKind::StandaloneFunction(predict_kind) => {
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen)
            }
        }
    }

    /// Call setup() on the predictor, handling weights parameter if present.
    ///
    /// Uses cog.predictor helpers to detect and extract weights:
    /// - `has_setup_weights()` checks if setup() has a weights parameter
    /// - `extract_setup_weights()` reads from COG_WEIGHTS env or ./weights path
    pub fn setup(&self, py: Python<'_>) -> PyResult<()> {
        let instance = self.instance.bind(py);

        // Check if setup method exists
        if !instance.hasattr("setup")? {
            return Ok(());
        }

        // Import cog.predictor helpers
        let cog_predictor = py.import("cog.predictor")?;
        let has_setup_weights = cog_predictor.getattr("has_setup_weights")?;
        let extract_setup_weights = cog_predictor.getattr("extract_setup_weights")?;

        // Check if setup() has a weights parameter
        let needs_weights: bool = has_setup_weights.call1((&instance,))?.extract()?;

        if needs_weights {
            // Extract weights from COG_WEIGHTS env or ./weights path
            let weights = extract_setup_weights.call1((&instance,))?;
            instance.call_method1("setup", (weights,))?;
        } else {
            instance.call_method0("setup")?;
        }

        Ok(())
    }

    /// Call predict() with the given input dict, returning raw Python output.
    ///
    /// For standalone functions, calls the function directly.
    pub fn predict_raw(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PyObject> {
        let (method_name, is_async) = match &self.kind {
            PredictorKind::Class { predict, .. } => (
                "predict",
                matches!(predict, PredictKind::Async | PredictKind::AsyncGen),
            ),
            PredictorKind::StandaloneFunction(predict_kind) => (
                "",
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen),
            ),
        };
        self.call_method_raw(py, method_name, is_async, input)
    }

    /// Call train() with the given input dict, returning raw Python output.
    ///
    /// For standalone train functions, calls the function directly.
    /// For Predictor classes with a train() method, calls instance.train().
    pub fn train_raw(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PyObject> {
        let (method_name, is_async) = match &self.kind {
            PredictorKind::Class { train, .. } => ("train", matches!(train, TrainKind::Async)),
            PredictorKind::StandaloneFunction(predict_kind) => (
                "",
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen),
            ),
        };
        self.call_method_raw(py, method_name, is_async, input)
    }

    /// Internal helper to call a method (predict or train) on the predictor.
    fn call_method_raw(
        &self,
        py: Python<'_>,
        method_name: &str,
        is_async: bool,
        input: &Bound<'_, PyDict>,
    ) -> PyResult<PyObject> {
        let instance = self.instance.bind(py);

        // For sync methods, enter cancelable state so SIGUSR1 can interrupt
        // The guard clears the flag on drop (even if we panic or error)
        let _cancelable_guard = if !is_async {
            Some(cancel::enter_cancelable())
        } else {
            None
        };

        // Call the method - returns coroutine if async, result if sync
        // If method_name is empty, call the instance directly (standalone function)
        let method_result = if method_name.is_empty() {
            instance.call((), Some(input))?
        } else {
            instance.call_method(method_name, (), Some(input))?
        };

        // If async, run the coroutine with asyncio.run()
        let result = if is_async {
            let asyncio = py.import("asyncio")?;
            asyncio.call_method1("run", (&method_result,))?
        } else {
            method_result
        };

        // Drop the cancelable guard now that the call is done
        drop(_cancelable_guard);

        Ok(result.unbind())
    }

    /// Worker mode predict - with input processing and output serialization.
    pub fn predict_worker(
        &self,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> Result<PredictionResult, PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;
            let types_module = py.import("types").map_err(|e| {
                PredictionError::Failed(format!("Failed to import types module: {}", e))
            })?;
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;

            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            // PreparedInput cleans up temp files on drop (RAII)
            let prepared = self
                .input_processor
                .prepare(py, raw_input_dict)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call predict
            let result = self.predict_raw(py, &input_dict);

            // Handle errors (prepared drops here, cleaning up temp files)
            let result = match result {
                Ok(r) => r,
                Err(e) => {
                    drop(prepared); // Explicit cleanup on error path
                    if is_cancelation_exception(py, &e) {
                        return Err(PredictionError::Cancelled);
                    }
                    return Err(PredictionError::Failed(format!("Prediction failed: {}", e)));
                }
            };

            let result_bound = result.bind(py);
            let is_generator: bool = result_bound.is_instance(&generator_type).unwrap_or(false);

            let output = if is_generator {
                self.process_generator_output(py, result_bound, &json_module, &slot_sender)?
            } else {
                self.process_single_output(py, result_bound, &json_module, &slot_sender)?
            };

            // prepared drops here, cleaning up temp files via RAII
            drop(prepared);

            Ok(PredictionResult {
                output,
                predict_time: None,
                logs: String::new(),
                metrics: Default::default(),
            })
        })
    }

    /// Worker mode train - with input processing and output serialization.
    pub fn train_worker(
        &self,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> Result<PredictionResult, PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;
            let types_module = py.import("types").map_err(|e| {
                PredictionError::Failed(format!("Failed to import types module: {}", e))
            })?;
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;

            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            // PreparedInput cleans up temp files on drop (RAII)
            let prepared = self
                .input_processor
                .prepare(py, raw_input_dict)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call train
            let result = self.train_raw(py, &input_dict);

            // Handle errors
            let result = match result {
                Ok(r) => r,
                Err(e) => {
                    drop(prepared);
                    if is_cancelation_exception(py, &e) {
                        return Err(PredictionError::Cancelled);
                    }
                    return Err(PredictionError::Failed(format!("Training failed: {}", e)));
                }
            };

            let result_bound = result.bind(py);
            let is_generator: bool = result_bound.is_instance(&generator_type).unwrap_or(false);

            let output = if is_generator {
                self.process_generator_output(py, result_bound, &json_module, &slot_sender)?
            } else {
                self.process_single_output(py, result_bound, &json_module, &slot_sender)?
            };

            drop(prepared);

            Ok(PredictionResult {
                output,
                predict_time: None,
                logs: String::new(),
                metrics: Default::default(),
            })
        })
    }

    /// Process generator output by streaming each yield over IPC.
    fn process_generator_output(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        json_module: &Bound<'_, PyAny>,
        slot_sender: &SlotSender,
    ) -> Result<PredictionOutput, PredictionError> {
        let iter = result
            .try_iter()
            .map_err(|e| PredictionError::Failed(format!("Failed to iterate generator: {}", e)))?;

        for item in iter {
            let item = item.map_err(|e| {
                if is_cancelation_exception(py, &e) {
                    return PredictionError::Cancelled;
                }
                PredictionError::Failed(format!("Generator iteration error: {}", e))
            })?;

            send_output_item(py, &item, json_module, slot_sender)?;
        }

        // Outputs already streamed over IPC — return empty stream
        Ok(PredictionOutput::Stream(vec![]))
    }

    /// Process single output into PredictionOutput::Single.
    ///
    /// For file outputs (Path/IOBase), the file is sent via slot_sender and
    /// an empty Single(Null) is returned since the output was already streamed.
    fn process_single_output(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        json_module: &Bound<'_, PyAny>,
        slot_sender: &SlotSender,
    ) -> Result<PredictionOutput, PredictionError> {
        // Check for file-type outputs first
        let os = py
            .import("os")
            .map_err(|e| PredictionError::Failed(format!("Failed to import os: {}", e)))?;
        let io_mod = py
            .import("io")
            .map_err(|e| PredictionError::Failed(format!("Failed to import io: {}", e)))?;
        let pathlike = os
            .getattr("PathLike")
            .map_err(|e| PredictionError::Failed(format!("Failed to get os.PathLike: {}", e)))?;
        let iobase = io_mod
            .getattr("IOBase")
            .map_err(|e| PredictionError::Failed(format!("Failed to get io.IOBase: {}", e)))?;

        if result.is_instance(&pathlike).unwrap_or(false) {
            let path_str: String = result
                .call_method0("__fspath__")
                .and_then(|p| p.extract())
                .map_err(|e| PredictionError::Failed(format!("Failed to get fspath: {}", e)))?;
            slot_sender
                .send_file_output(std::path::PathBuf::from(path_str), None)
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to send file output: {}", e))
                })?;
            return Ok(PredictionOutput::Single(serde_json::Value::Null));
        }

        if result.is_instance(&iobase).unwrap_or(false) {
            if result
                .call_method0("seekable")
                .and_then(|r| r.extract::<bool>())
                .unwrap_or(false)
            {
                let _ = result.call_method1("seek", (0,));
            }
            let data: Vec<u8> = result
                .call_method0("read")
                .and_then(|d| d.extract())
                .map_err(|e| PredictionError::Failed(format!("Failed to read IOBase: {}", e)))?;
            let ext = result
                .getattr("name")
                .and_then(|n| n.extract::<String>())
                .ok()
                .and_then(|name| {
                    std::path::Path::new(&name)
                        .extension()
                        .and_then(|e| e.to_str())
                        .map(|s| s.to_string())
                })
                .unwrap_or_else(|| "bin".to_string());
            slot_sender
                .write_file_output(&data, &ext, None)
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to write file output: {}", e))
                })?;
            return Ok(PredictionOutput::Single(serde_json::Value::Null));
        }

        // Non-file output — process normally
        let processed = output::process_output(py, result, None)
            .map_err(|e| PredictionError::Failed(format!("Failed to process output: {}", e)))?;

        let result_str: String = json_module
            .call_method1("dumps", (&processed,))
            .map_err(|e| PredictionError::Failed(format!("Failed to serialize output: {}", e)))?
            .extract()
            .map_err(|e| {
                PredictionError::Failed(format!("Failed to extract output string: {}", e))
            })?;

        let output_json: serde_json::Value = serde_json::from_str(&result_str)
            .map_err(|e| PredictionError::Failed(format!("Failed to parse output JSON: {}", e)))?;

        Ok(PredictionOutput::Single(output_json))
    }

    /// Worker mode async predict - submits to shared event loop.
    ///
    /// Uses run_coroutine_threadsafe to submit the coroutine to the provided event loop.
    /// Returns the concurrent.futures.Future, is_async_gen flag, and PreparedInput for cleanup.
    /// Caller should block on future.result() to get the result, then drop PreparedInput.
    ///
    /// The prediction_id is used to set up log routing in the event loop thread.
    pub fn predict_async_worker(
        &self,
        input: serde_json::Value,
        event_loop: &Py<PyAny>,
        prediction_id: &str,
    ) -> Result<(Py<PyAny>, bool, PreparedInput), PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;
            let asyncio = py
                .import("asyncio")
                .map_err(|e| PredictionError::Failed(format!("Failed to import asyncio: {}", e)))?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;
            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            let prepared = self
                .input_processor
                .prepare(py, raw_input_dict)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call predict - returns coroutine
            let instance = self.instance.bind(py);
            let coro = instance
                .call_method("predict", (), Some(&input_dict))
                .map_err(|e| PredictionError::Failed(format!("Failed to call predict: {}", e)))?;

            // For async generators, wrap to collect all values
            let is_async_gen = matches!(
                &self.kind,
                PredictorKind::Class {
                    predict: PredictKind::AsyncGen,
                    ..
                } | PredictorKind::StandaloneFunction(PredictKind::AsyncGen)
            );
            let coro = if is_async_gen {
                let collect_fn = get_collect_async_gen(py)?;
                collect_fn
                    .call1(py, (&coro,))
                    .map_err(|e| {
                        PredictionError::Failed(format!("Failed to wrap async generator: {}", e))
                    })?
                    .into_bound(py)
            } else {
                coro
            };

            // Wrap coroutine to set up log routing in the event loop thread
            let ctx_wrapper = get_ctx_wrapper(py)?;

            // Get the same ContextVar instance used by SlotLogWriter for log routing
            let contextvar = crate::log_writer::get_prediction_contextvar(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get prediction ContextVar: {}", e))
            })?;

            // Wrap the coroutine with context setup
            let wrapped_coro = ctx_wrapper
                .call1(py, (&coro, prediction_id, contextvar.bind(py)))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to wrap coroutine with context: {}", e))
                })?;

            // Submit wrapped coroutine to shared event loop via run_coroutine_threadsafe
            let future = asyncio
                .call_method1(
                    "run_coroutine_threadsafe",
                    (wrapped_coro.bind(py), event_loop.bind(py)),
                )
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to submit coroutine: {}", e))
                })?;

            Ok((future.unbind(), is_async_gen, prepared))
        })
    }

    /// Process the result from an async prediction future.
    ///
    /// Call this after future.result() returns to convert the Python result
    /// to a PredictionResult.
    pub fn process_async_result(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        is_async_gen: bool,
        slot_sender: &SlotSender,
    ) -> Result<PredictionResult, PredictionError> {
        let json_module = py
            .import("json")
            .map_err(|e| PredictionError::Failed(format!("Failed to import json module: {}", e)))?;
        let types_module = py.import("types").map_err(|e| {
            PredictionError::Failed(format!("Failed to import types module: {}", e))
        })?;

        // Process output
        let output = if is_async_gen {
            // Result is a pre-collected list — stream each item over IPC
            if let Ok(list) = result.extract::<Vec<Bound<'_, PyAny>>>() {
                for item in list {
                    send_output_item(py, &item, &json_module, slot_sender)?;
                }
            }
            PredictionOutput::Stream(vec![])
        } else {
            // Check if result is a generator (sync generator from async predict)
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;
            let is_generator: bool = result.is_instance(&generator_type).unwrap_or(false);

            if is_generator {
                self.process_generator_output(py, result, &json_module, slot_sender)?
            } else {
                self.process_single_output(py, result, &json_module, slot_sender)?
            }
        };

        Ok(PredictionResult {
            output,
            predict_time: None,
            logs: String::new(),
            metrics: Default::default(),
        })
    }

    /// Worker mode async train - submits to shared event loop.
    pub fn train_async_worker(
        &self,
        input: serde_json::Value,
        event_loop: &Py<PyAny>,
        prediction_id: &str,
    ) -> Result<(Py<PyAny>, bool, PreparedInput), PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;
            let asyncio = py
                .import("asyncio")
                .map_err(|e| PredictionError::Failed(format!("Failed to import asyncio: {}", e)))?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;
            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            let prepared = self
                .input_processor
                .prepare(py, raw_input_dict)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call train - returns coroutine
            let instance = self.instance.bind(py);
            let coro = match &self.kind {
                PredictorKind::StandaloneFunction(_) => instance.call((), Some(&input_dict)),
                PredictorKind::Class { .. } => instance.call_method("train", (), Some(&input_dict)),
            }
            .map_err(|e| PredictionError::Failed(format!("Failed to call train: {}", e)))?;

            // Wrap coroutine to set up log routing
            let ctx_wrapper = get_ctx_wrapper(py)?;

            // Get the same ContextVar instance used by SlotLogWriter
            let contextvar = crate::log_writer::get_prediction_contextvar(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get prediction ContextVar: {}", e))
            })?;

            // Wrap the coroutine with context setup
            let wrapped_coro = ctx_wrapper
                .call1(py, (&coro, prediction_id, contextvar.bind(py)))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to wrap coroutine with context: {}", e))
                })?;

            // Submit wrapped coroutine to shared event loop
            let future = asyncio
                .call_method1(
                    "run_coroutine_threadsafe",
                    (wrapped_coro.bind(py), event_loop.bind(py)),
                )
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to submit coroutine: {}", e))
                })?;

            // Train doesn't typically use async generators, but we return false for consistency
            Ok((future.unbind(), false, prepared))
        })
    }

    // =========================================================================
    // Healthcheck methods
    // =========================================================================

    /// Healthcheck timeout in seconds.
    const HEALTHCHECK_TIMEOUT: f64 = 5.0;

    /// Check if the predictor has a healthcheck() method.
    pub fn has_healthcheck(&self, py: Python<'_>) -> bool {
        match &self.kind {
            PredictorKind::Class { .. } => {
                let instance = self.instance.bind(py);
                instance.hasattr("healthcheck").unwrap_or(false)
            }
            PredictorKind::StandaloneFunction(_) => false,
        }
    }

    /// Check if the healthcheck() method is async.
    pub fn is_healthcheck_async(&self, py: Python<'_>) -> bool {
        match &self.kind {
            PredictorKind::Class { .. } => {
                let instance = self.instance.bind(py);
                if let Ok(healthcheck) = instance.getattr("healthcheck") {
                    let inspect = py.import("inspect").ok();
                    if let Some(inspect) = inspect {
                        inspect
                            .call_method1("iscoroutinefunction", (&healthcheck,))
                            .ok()
                            .and_then(|r| r.extract::<bool>().ok())
                            .unwrap_or(false)
                    } else {
                        false
                    }
                } else {
                    false
                }
            }
            PredictorKind::StandaloneFunction(_) => false,
        }
    }

    /// Run a synchronous healthcheck with timeout.
    ///
    /// Runs the healthcheck in a thread pool executor with a 5 second timeout.
    pub fn healthcheck_sync(&self, py: Python<'_>) -> coglet_core::orchestrator::HealthcheckResult {
        use coglet_core::orchestrator::HealthcheckResult;

        let instance = self.instance.bind(py);

        // Run healthcheck in executor with timeout, mirroring Python impl
        let result: PyResult<bool> = (|| {
            let concurrent_futures = py.import("concurrent.futures")?;
            let thread_pool = concurrent_futures.getattr("ThreadPoolExecutor")?;

            // Create a small executor just for this healthcheck
            let executor = thread_pool.call1((1,))?;

            // Get the healthcheck method
            let healthcheck_fn = instance.getattr("healthcheck")?;

            // Submit to executor
            let future = executor.call_method1("submit", (healthcheck_fn,))?;

            // Wait with timeout
            let result = future.call_method1("result", (Self::HEALTHCHECK_TIMEOUT,));

            // Shutdown executor
            let _ = executor.call_method1("shutdown", (false,));

            match result {
                Ok(r) => Ok(r.extract::<bool>().unwrap_or(true)),
                Err(e) => {
                    let err_str = e.to_string();
                    if err_str.contains("TimeoutError") {
                        Err(pyo3::exceptions::PyTimeoutError::new_err(
                            "Healthcheck timed out",
                        ))
                    } else {
                        Err(e)
                    }
                }
            }
        })();

        match result {
            Ok(true) => HealthcheckResult::healthy(),
            Ok(false) => HealthcheckResult::unhealthy(
                "Healthcheck failed: user-defined healthcheck returned False",
            ),
            Err(e) => {
                let err_str = e.to_string();
                if err_str.contains("TimeoutError") {
                    HealthcheckResult::unhealthy(format!(
                        "Healthcheck failed: user-defined healthcheck timed out after {:.1} seconds",
                        Self::HEALTHCHECK_TIMEOUT
                    ))
                } else {
                    HealthcheckResult::unhealthy(format!("Healthcheck failed: {}", e))
                }
            }
        }
    }

    /// Run an async healthcheck with timeout.
    ///
    /// Runs the healthcheck in the async event loop with a 5 second timeout.
    pub fn healthcheck_async(
        &self,
        py: Python<'_>,
        event_loop: &Py<PyAny>,
    ) -> coglet_core::orchestrator::HealthcheckResult {
        use coglet_core::orchestrator::HealthcheckResult;

        let instance = self.instance.bind(py);

        let result: PyResult<bool> = (|| {
            let asyncio = py.import("asyncio")?;

            // Get the healthcheck coroutine
            let healthcheck_fn = instance.getattr("healthcheck")?;
            let coro = healthcheck_fn.call0()?;

            // Wrap with timeout
            let wait_for = asyncio.getattr("wait_for")?;
            let timeout_coro = wait_for.call1((&coro, Self::HEALTHCHECK_TIMEOUT))?;

            // Submit to event loop
            let future = asyncio.call_method1(
                "run_coroutine_threadsafe",
                (&timeout_coro, event_loop.bind(py)),
            )?;

            // Block on result with extra buffer time for event loop overhead
            let result = future.call_method1("result", (Self::HEALTHCHECK_TIMEOUT + 1.0,));

            match result {
                Ok(r) => Ok(r.extract::<bool>().unwrap_or(true)),
                Err(e) => {
                    let err_str = e.to_string();
                    if err_str.contains("TimeoutError") || err_str.contains("timed out") {
                        Err(pyo3::exceptions::PyTimeoutError::new_err(
                            "Healthcheck timed out",
                        ))
                    } else {
                        Err(e)
                    }
                }
            }
        })();

        match result {
            Ok(true) => HealthcheckResult::healthy(),
            Ok(false) => HealthcheckResult::unhealthy(
                "Healthcheck failed: user-defined healthcheck returned False",
            ),
            Err(e) => {
                let err_str = e.to_string();
                if err_str.contains("TimeoutError") {
                    HealthcheckResult::unhealthy(format!(
                        "Healthcheck failed: user-defined healthcheck timed out after {:.1} seconds",
                        Self::HEALTHCHECK_TIMEOUT
                    ))
                } else {
                    HealthcheckResult::unhealthy(format!("Healthcheck failed: {}", e))
                }
            }
        }
    }
}
