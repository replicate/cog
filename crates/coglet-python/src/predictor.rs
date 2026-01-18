//! Python predictor loading and invocation.

use pyo3::prelude::*;
use pyo3::types::PyDict;

use coglet_core::{PredictionError, PredictionOutput, PredictionResult};

use crate::cancel;
use crate::input::{self, InputProcessor, PreparedInput, Runtime};
use crate::output;

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

/// Type alias for Python object (Py<PyAny>).
type PyObject = Py<PyAny>;

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
/// Predictors can come from different runtimes:
/// - **Pydantic (cog)**: Uses Pydantic BaseModel, URLPath for file downloads
/// - **Coglet**: Uses dataclasses and ADT types
///
/// We detect the runtime on load and use the appropriate input processor.
pub struct PythonPredictor {
    instance: PyObject,
    /// Whether predict() is an async function (coroutine or async generator).
    is_async: bool,
    /// Whether predict() is an async generator function specifically.
    is_async_gen: bool,
    /// Whether this predictor has a train() method.
    has_train: bool,
    /// Whether train() is an async function (if has_train is true).
    is_train_async: bool,
    /// Whether the instance is a standalone function (not a class instance).
    /// Used for standalone train functions like `train.py:train`.
    is_standalone_function: bool,
    /// The detected runtime type.
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

        // For standalone functions (like train functions), detect async on the function itself
        // For Predictor instances, detect async on the predict() method
        let (is_async, is_async_gen) = if is_function {
            Self::detect_async(py, &instance, "")? // Empty string means check the function itself
        } else {
            Self::detect_async(py, &instance, "predict")?
        };
        // Only log predict() detection for Predictor classes (not standalone train functions)
        if !is_function {
            if is_async_gen {
                tracing::info!("Detected async generator predict()");
            } else if is_async {
                tracing::info!("Detected async predict()");
            } else {
                tracing::info!("Detected sync predict()");
            }
        }

        // Check if train() method exists and if it's async.
        // For standalone functions (is_function=true), the function itself IS the train
        // function, so we consider has_train=true and use is_async for train_async.
        let has_train = if is_function {
            true // Standalone function is the train function
        } else {
            instance.bind(py).hasattr("train")?
        };
        let is_train_async = if is_function {
            // For standalone functions, async status was already detected above
            // Log the train function detection here
            if is_async {
                tracing::info!("Detected async train()");
            } else {
                tracing::info!("Detected sync train()");
            }
            is_async
        } else if has_train {
            let (train_async, _) = Self::detect_async(py, &instance, "train")?;
            if train_async {
                tracing::info!("Detected async train()");
            } else {
                tracing::info!("Detected sync train()");
            }
            train_async
        } else {
            false
        };

        // Detect runtime and create input processor
        let runtime = input::detect_runtime(py, predictor_ref, &instance).map_err(|e| {
            pyo3::exceptions::PyRuntimeError::new_err(e.to_string())
        })?;
        let input_processor = input::create_input_processor(&runtime);

        Ok(Self {
            instance,
            is_async,
            is_async_gen,
            has_train,
            is_train_async,
            is_standalone_function: is_function,
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
        self.is_async
    }

    /// Returns true if this predictor has a train() method.
    pub fn has_train(&self) -> bool {
        self.has_train
    }

    /// Returns true if the train() method is async.
    pub fn is_train_async(&self) -> bool {
        self.is_train_async
    }

    /// Generate OpenAPI schema for this predictor.
    ///
    /// Uses coglet.schemas.to_json_schema() which returns the full OpenAPI spec
    /// with Input/Output schemas populated from the predictor's type annotations.
    ///
    /// Returns None if schema generation fails (best-effort).
    pub fn schema(&self) -> Option<serde_json::Value> {
        Python::attach(|py| {
            // Try coglet schema generation first (works for both runtimes)
            let result: PyResult<serde_json::Value> = (|| {
                let json_module = py.import("json")?;

                // For Coglet runtime, we have the ADT predictor directly
                // For Pydantic runtime, we need to create the ADT predictor from the class
                let adt_predictor = match &self.runtime {
                    Runtime::Coglet { adt_predictor } => adt_predictor.bind(py).clone(),
                    Runtime::Pydantic { input_type: _ } => {
                        // For Pydantic, we need to introspect the predictor class
                        // Use coglet.inspector.create_predictor equivalent
                        // This is complex, so for now just use cog's FastAPI schema
                        return self.schema_via_fastapi(py, json_module.as_any());
                    }
                };

                // Use coglet.schemas.to_json_schema(adt_predictor)
                let schemas_module = py.import("coglet.schemas")?;
                let to_json_schema = schemas_module.getattr("to_json_schema")?;
                let schema = to_json_schema.call1((&adt_predictor,))?;

                // Convert to JSON string then parse to serde_json::Value
                let schema_str: String =
                    json_module.call_method1("dumps", (&schema,))?.extract()?;

                let schema_value: serde_json::Value = serde_json::from_str(&schema_str)
                    .map_err(|e| pyo3::exceptions::PyValueError::new_err(e.to_string()))?;

                Ok(schema_value)
            })();

            match result {
                Ok(schema) => Some(schema),
                Err(e) => {
                    tracing::warn!(error = %e, "Failed to generate OpenAPI schema");
                    None
                }
            }
        })
    }

    /// Generate schema via FastAPI (fallback for Pydantic predictors).
    fn schema_via_fastapi(
        &self,
        py: Python<'_>,
        json_module: &Bound<'_, PyAny>,
    ) -> PyResult<serde_json::Value> {
        // For Pydantic runtime, use cog's FastAPI app to generate schema
        // This is what cog.command.openapi_schema does
        let cog_server_http = py.import("cog.server.http")?;
        let create_app = cog_server_http.getattr("create_app")?;

        // Need to pass a Config - try to load from cog.yaml
        let cog_config_module = py.import("cog.config")?;
        let config_class = cog_config_module.getattr("Config")?;
        let config = config_class.call0()?;

        // Create app with is_build=True to skip actual setup
        let kwargs = pyo3::types::PyDict::new(py);
        kwargs.set_item("cog_config", &config)?;
        kwargs.set_item("shutdown_event", py.None())?;
        kwargs.set_item("is_build", true)?;

        let app = create_app.call((), Some(&kwargs))?;

        // Get OpenAPI schema from app
        let openapi_method = app.getattr("openapi")?;
        let schema = openapi_method.call0()?;

        // Convert to JSON string then parse
        let schema_str: String = json_module.call_method1("dumps", (&schema,))?.extract()?;

        let schema_value: serde_json::Value = serde_json::from_str(&schema_str)
            .map_err(|e| pyo3::exceptions::PyValueError::new_err(e.to_string()))?;

        Ok(schema_value)
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
    /// For standalone functions (is_standalone_function=true), calls the function directly.
    pub fn predict_raw(
        &self,
        py: Python<'_>,
        input: &Bound<'_, PyDict>,
    ) -> PyResult<PyObject> {
        // For standalone functions, use empty method_name to call directly
        let method_name = if self.is_standalone_function {
            ""
        } else {
            "predict"
        };
        self.call_method_raw(py, method_name, self.is_async, input)
    }

    /// Call train() with the given input dict, returning raw Python output.
    ///
    /// For standalone train functions (is_standalone_function=true), calls the function directly.
    /// For Predictor classes with a train() method, calls instance.train().
    pub fn train_raw(
        &self,
        py: Python<'_>,
        input: &Bound<'_, PyDict>,
    ) -> PyResult<PyObject> {
        // For standalone functions, use empty method_name to call directly
        // and use is_async (the function's async status) instead of is_train_async
        let (method_name, is_async) = if self.is_standalone_function {
            ("", self.is_async)
        } else {
            ("train", self.is_train_async)
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
                .map_err(|e| {
                    PredictionError::InvalidInput(format!("Input validation failed: {}", e))
                })?;
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
                self.process_generator_output(py, result_bound, &json_module)?
            } else {
                self.process_single_output(py, result_bound, &json_module)?
            };

            // prepared drops here, cleaning up temp files via RAII
            drop(prepared);

            Ok(PredictionResult {
                output,
                predict_time: None,
                logs: String::new(),
            })
        })
    }

    /// Worker mode train - with input processing and output serialization.
    pub fn train_worker(
        &self,
        input: serde_json::Value,
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
                .map_err(|e| {
                    PredictionError::InvalidInput(format!("Input validation failed: {}", e))
                })?;
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
                self.process_generator_output(py, result_bound, &json_module)?
            } else {
                self.process_single_output(py, result_bound, &json_module)?
            };

            drop(prepared);

            Ok(PredictionResult {
                output,
                predict_time: None,
                logs: String::new(),
            })
        })
    }

    /// Process generator output into PredictionOutput::Stream.
    fn process_generator_output(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        json_module: &Bound<'_, PyAny>,
    ) -> Result<PredictionOutput, PredictionError> {
        let mut outputs = Vec::new();
        let iter = result.try_iter().map_err(|e| {
            PredictionError::Failed(format!("Failed to iterate generator: {}", e))
        })?;

        for item in iter {
            let item = item.map_err(|e| {
                if is_cancelation_exception(py, &e) {
                    return PredictionError::Cancelled;
                }
                PredictionError::Failed(format!("Generator iteration error: {}", e))
            })?;

            let processed = output::process_output_item(py, &item).map_err(|e| {
                PredictionError::Failed(format!("Failed to process output item: {}", e))
            })?;

            let item_str: String = json_module
                .call_method1("dumps", (&processed,))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to serialize output item: {}", e))
                })?
                .extract()
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to extract output string: {}", e))
                })?;

            let item_json: serde_json::Value = serde_json::from_str(&item_str).map_err(|e| {
                PredictionError::Failed(format!("Failed to parse output JSON: {}", e))
            })?;

            outputs.push(item_json);
        }

        Ok(PredictionOutput::Stream(outputs))
    }

    /// Process single output into PredictionOutput::Single.
    fn process_single_output(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        json_module: &Bound<'_, PyAny>,
    ) -> Result<PredictionOutput, PredictionError> {
        let processed = output::process_output(py, result, None).map_err(|e| {
            PredictionError::Failed(format!("Failed to process output: {}", e))
        })?;

        let result_str: String = json_module
            .call_method1("dumps", (&processed,))
            .map_err(|e| PredictionError::Failed(format!("Failed to serialize output: {}", e)))?
            .extract()
            .map_err(|e| PredictionError::Failed(format!("Failed to extract output string: {}", e)))?;

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
                .map_err(|e| {
                    PredictionError::InvalidInput(format!("Input validation failed: {}", e))
                })?;
            let input_dict = prepared.dict(py);

            // Call predict - returns coroutine
            let instance = self.instance.bind(py);
            let coro = instance
                .call_method("predict", (), Some(&input_dict))
                .map_err(|e| PredictionError::Failed(format!("Failed to call predict: {}", e)))?;

            // For async generators, wrap to collect all values
            let coro = if self.is_async_gen {
                let collect_code = "
async def _collect_async_gen(agen):
    results = []
    async for item in agen:
        results.append(item)
    return results
";
                let builtins = py.import("builtins").map_err(|e| {
                    PredictionError::Failed(format!("Failed to import builtins: {}", e))
                })?;
                let exec_fn = builtins
                    .getattr("exec")
                    .map_err(|e| PredictionError::Failed(format!("Failed to get exec: {}", e)))?;
                let globals = PyDict::new(py);
                exec_fn.call1((collect_code, &globals)).map_err(|e| {
                    PredictionError::Failed(format!("Failed to define collect helper: {}", e))
                })?;
                let collect_fn = globals
                    .get_item("_collect_async_gen")
                    .map_err(|e| {
                        PredictionError::Failed(format!("Failed to get collect helper: {}", e))
                    })?
                    .ok_or_else(|| {
                        PredictionError::Failed("_collect_async_gen not found".to_string())
                    })?;
                collect_fn.call1((&coro,)).map_err(|e| {
                    PredictionError::Failed(format!("Failed to wrap async generator: {}", e))
                })?
            } else {
                coro
            };

            // Wrap coroutine to set up log routing in the event loop thread
            let wrap_code = r#"
async def _ctx_wrapper(coro, prediction_id, contextvar):
    contextvar.set(prediction_id)
    return await coro
"#;
            let builtins = py.import("builtins").map_err(|e| {
                PredictionError::Failed(format!("Failed to import builtins: {}", e))
            })?;
            let exec_fn = builtins.getattr("exec").map_err(|e| {
                PredictionError::Failed(format!("Failed to get exec: {}", e))
            })?;
            let globals = PyDict::new(py);
            exec_fn.call1((wrap_code, &globals)).map_err(|e| {
                PredictionError::Failed(format!("Failed to define context wrapper: {}", e))
            })?;
            let ctx_wrapper = globals
                .get_item("_ctx_wrapper")
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to get context wrapper: {}", e))
                })?
                .ok_or_else(|| PredictionError::Failed("_ctx_wrapper not found".to_string()))?;

            // Get the same ContextVar instance used by SlotLogWriter for log routing
            let contextvar = crate::log_writer::get_prediction_contextvar(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get prediction ContextVar: {}", e))
            })?;

            // Wrap the coroutine with context setup
            let wrapped_coro = ctx_wrapper
                .call1((&coro, prediction_id, contextvar.bind(py)))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to wrap coroutine with context: {}", e))
                })?;

            // Submit wrapped coroutine to shared event loop via run_coroutine_threadsafe
            let future = asyncio
                .call_method1("run_coroutine_threadsafe", (&wrapped_coro, event_loop.bind(py)))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to submit coroutine: {}", e))
                })?;

            Ok((future.unbind(), self.is_async_gen, prepared))
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
    ) -> Result<PredictionResult, PredictionError> {
        let json_module = py
            .import("json")
            .map_err(|e| PredictionError::Failed(format!("Failed to import json module: {}", e)))?;
        let types_module = py.import("types").map_err(|e| {
            PredictionError::Failed(format!("Failed to import types module: {}", e))
        })?;

        // Process output
        let output = if is_async_gen {
            // Result is a list
            let mut outputs = Vec::new();
            if let Ok(list) = result.extract::<Vec<Bound<'_, PyAny>>>() {
                for item in list {
                    let processed = output::process_output_item(py, &item).map_err(|e| {
                        PredictionError::Failed(format!("Failed to process output item: {}", e))
                    })?;
                    let item_str: String = json_module
                        .call_method1("dumps", (&processed,))
                        .map_err(|e| {
                            PredictionError::Failed(format!("Failed to serialize: {}", e))
                        })?
                        .extract()
                        .map_err(|e| {
                            PredictionError::Failed(format!("Failed to extract: {}", e))
                        })?;
                    let item_json: serde_json::Value = serde_json::from_str(&item_str)
                        .map_err(|e| PredictionError::Failed(format!("Failed to parse: {}", e)))?;
                    outputs.push(item_json);
                }
            }
            PredictionOutput::Stream(outputs)
        } else {
            // Check if result is a generator (sync generator from async predict)
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;
            let is_generator: bool = result.is_instance(&generator_type).unwrap_or(false);

            if is_generator {
                self.process_generator_output(py, result, &json_module)?
            } else {
                self.process_single_output(py, result, &json_module)?
            }
        };

        Ok(PredictionResult {
            output,
            predict_time: None,
            logs: String::new(),
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
                .map_err(|e| {
                    PredictionError::InvalidInput(format!("Input validation failed: {}", e))
                })?;
            let input_dict = prepared.dict(py);

            // Call train - returns coroutine
            let instance = self.instance.bind(py);
            let coro = if self.is_standalone_function {
                instance.call((), Some(&input_dict))
            } else {
                instance.call_method("train", (), Some(&input_dict))
            }
            .map_err(|e| PredictionError::Failed(format!("Failed to call train: {}", e)))?;

            // Wrap coroutine to set up log routing
            let wrap_code = r#"
async def _ctx_wrapper(coro, prediction_id, contextvar):
    contextvar.set(prediction_id)
    return await coro
"#;
            let builtins = py.import("builtins").map_err(|e| {
                PredictionError::Failed(format!("Failed to import builtins: {}", e))
            })?;
            let exec_fn = builtins.getattr("exec").map_err(|e| {
                PredictionError::Failed(format!("Failed to get exec: {}", e))
            })?;
            let globals = PyDict::new(py);
            exec_fn.call1((wrap_code, &globals)).map_err(|e| {
                PredictionError::Failed(format!("Failed to define context wrapper: {}", e))
            })?;
            let ctx_wrapper = globals
                .get_item("_ctx_wrapper")
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to get context wrapper: {}", e))
                })?
                .ok_or_else(|| PredictionError::Failed("_ctx_wrapper not found".to_string()))?;

            // Get the same ContextVar instance used by SlotLogWriter
            let contextvar = crate::log_writer::get_prediction_contextvar(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get prediction ContextVar: {}", e))
            })?;

            // Wrap the coroutine with context setup
            let wrapped_coro = ctx_wrapper
                .call1((&coro, prediction_id, contextvar.bind(py)))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to wrap coroutine with context: {}", e))
                })?;

            // Submit wrapped coroutine to shared event loop
            let future = asyncio
                .call_method1("run_coroutine_threadsafe", (&wrapped_coro, event_loop.bind(py)))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to submit coroutine: {}", e))
                })?;

            // Train doesn't typically use async generators, but we return false for consistency
            Ok((future.unbind(), false, prepared))
        })
    }
}
