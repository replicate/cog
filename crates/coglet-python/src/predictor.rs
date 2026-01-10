//! Python predictor loading and invocation.

use std::sync::OnceLock;
use std::thread;

use pyo3::prelude::*;
use pyo3::types::PyDict;

use coglet_core::{PredictionError, PredictionOutput, PredictionResult};

use crate::cancel;
use crate::input::{self, InputProcessor, Runtime};
use crate::output;

/// Check if a PyErr is a CancelationException or asyncio.CancelledError.
fn is_cancelation_exception(py: Python<'_>, err: &PyErr) -> bool {
    // Check for cog.server.exceptions.CancelationException
    if let Ok(exceptions) = py.import("cog.server.exceptions") {
        if let Ok(cancel_exc) = exceptions.getattr("CancelationException") {
            if err.is_instance(py, &cancel_exc) {
                return true;
            }
        }
    }

    // Check for asyncio.CancelledError
    if let Ok(asyncio) = py.import("asyncio") {
        if let Ok(cancelled_error) = asyncio.getattr("CancelledError") {
            if err.is_instance(py, &cancelled_error) {
                return true;
            }
        }
    }

    false
}

/// Type alias for Python object (Py<PyAny>).
type PyObject = Py<PyAny>;

/// Global Python asyncio event loop running in a dedicated thread.
/// This is initialized once when the first async predictor is used.
static ASYNCIO_EVENT_LOOP: OnceLock<PyObject> = OnceLock::new();

/// Initialize and start the Python asyncio event loop in a background thread.
/// Returns a reference to the event loop that can be used with `into_future_with_locals`.
fn get_or_init_event_loop(py: Python<'_>) -> PyResult<&'static PyObject> {
    // Check if already initialized
    if let Some(event_loop) = ASYNCIO_EVENT_LOOP.get() {
        return Ok(event_loop);
    }
    
    let asyncio = py.import("asyncio")?;
    
    // Create a new event loop
    let event_loop = asyncio.call_method0("new_event_loop")?;
    let event_loop_obj: PyObject = event_loop.unbind();
    
    // Clone for the thread (need py token for clone_ref)
    let loop_for_thread = event_loop_obj.clone_ref(py);
    
    // Start the event loop in a dedicated background thread
    thread::spawn(move || {
        Python::attach(|py| {
            let event_loop = loop_for_thread.bind(py);
            
            // Set this as the event loop for this thread
            let asyncio = py.import("asyncio").expect("Failed to import asyncio");
            asyncio.call_method1("set_event_loop", (event_loop,))
                .expect("Failed to set event loop");
            
            // Run the event loop forever (blocks this thread)
            tracing::debug!("Starting asyncio event loop in background thread");
            if let Err(e) = event_loop.call_method0("run_forever") {
                tracing::error!("Event loop error: {}", e);
            }
        });
    });
    
    tracing::info!("Initialized asyncio event loop in background thread");
    
    // Store and return
    // Race condition is fine - worst case we create an extra loop that gets dropped
    let _ = ASYNCIO_EVENT_LOOP.set(event_loop_obj);
    Ok(ASYNCIO_EVENT_LOOP.get().unwrap())
}

/// A loaded Python predictor instance.
///
/// # GIL and Concurrency
///
/// This struct wraps a Python predictor object. The concurrency model depends on
/// the Python runtime:
///
/// ## GIL Python (default, 3.8-3.12, 3.13 default)
/// - `Python::with_gil()` acquires the GIL before calling into Python
/// - Only one thread can execute Python bytecode at a time
/// - However, native extensions (torch, numpy) release the GIL during compute
/// - CUDA operations in torch run without holding GIL, allowing I/O concurrency
/// - For sync predictors, max_concurrency=1 is appropriate
///
/// ## Free-threaded Python (3.13t+)
/// - No GIL, multiple threads can run Python simultaneously  
/// - `Python::with_gil()` still works but doesn't serialize execution
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
///
/// # Current Implementation
///
/// We detect async predictors via `inspect.iscoroutinefunction()` and handle
/// them by running them in Python's asyncio event loop.
pub struct PythonPredictor {
    instance: PyObject,
    /// Whether predict() is an async function (coroutine or async generator).
    is_async: bool,
    /// Whether predict() is an async generator function specifically.
    is_async_gen: bool,
    /// The detected runtime type.
    runtime: Runtime,
    /// Input processor for this runtime.
    input_processor: Box<dyn InputProcessor>,
}

// PyObject is Send in PyO3 0.23+
// Safety: We only access the instance through Python::with_gil()
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

        // Check if predict() is async (coroutine or async generator)
        let (is_async, is_async_gen) = Self::detect_async(py, &instance)?;
        if is_async_gen {
            tracing::info!("Detected async generator predict()");
        } else if is_async {
            tracing::info!("Detected async predict()");
        }

        // Detect runtime and create input processor
        let runtime = input::detect_runtime(py, predictor_ref, &instance);
        let input_processor = input::create_input_processor(&runtime);

        Ok(Self {
            instance,
            is_async,
            is_async_gen,
            runtime,
            input_processor,
        })
    }

    /// Detect if predict() is an async function.
    /// Returns (is_async, is_async_gen) tuple.
    fn detect_async(py: Python<'_>, instance: &PyObject) -> PyResult<(bool, bool)> {
        let inspect = py.import("inspect")?;
        let predict = instance.bind(py).getattr("predict")?;
        
        // Check isasyncgenfunction first (it's more specific)
        let is_async_gen: bool = inspect.call_method1("isasyncgenfunction", (&predict,))?.extract()?;
        if is_async_gen {
            return Ok((true, true));
        }
        
        // Check iscoroutinefunction
        let is_coro: bool = inspect.call_method1("iscoroutinefunction", (&predict,))?.extract()?;
        Ok((is_coro, false))
    }

    /// Returns true if this predictor has an async predict() method.
    pub fn is_async(&self) -> bool {
        self.is_async
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

    /// Call predict() with the given input dict, returning JSON-serializable output.
    /// Captures stdout/stderr and logs them via tracing.
    pub fn predict_raw(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PyObject> {
        let instance = self.instance.bind(py);

        // Import the stream redirector
        let helpers = py.import("cog.server.helpers")?;
        let redirector_class = helpers.getattr("SimpleStreamRedirector")?;

        // Create a Python callback that logs to tracing
        // For now, we'll collect output and log after
        let logs: std::sync::Arc<std::sync::Mutex<Vec<(String, String)>>> =
            std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
        let logs_clone = logs.clone();

        // Create a simple Python function to capture logs
        let callback = pyo3::types::PyCFunction::new_closure(
            py,
            None,
            None,
            move |args: &Bound<'_, pyo3::types::PyTuple>, _kwargs: Option<&Bound<'_, PyDict>>| -> PyResult<()> {
                let source: String = args.get_item(0)?.extract()?;
                let text: String = args.get_item(1)?.extract()?;
                if let Ok(mut guard) = logs_clone.lock() {
                    guard.push((source, text));
                }
                Ok(())
            },
        )?;

        // Create redirector with tee=true so output still goes to console
        let redirector = redirector_class.call1((callback, true))?;

        // Use redirector as context manager
        let result = redirector.call_method0("__enter__")?;
        let _ = result; // redirector returns self

        // For sync predictors, enter cancelable state so SIGUSR1 can interrupt
        // The guard clears the flag on drop (even if we panic or error)
        let _cancelable_guard = if !self.is_async {
            Some(cancel::enter_cancelable())
        } else {
            None
        };

        // Call predict - returns coroutine if async, result if sync
        let predict_result = instance.call_method("predict", (), Some(input))?;

        // If async, run the coroutine with asyncio.run()
        let result = if self.is_async {
            let asyncio = py.import("asyncio")?;
            asyncio.call_method1("run", (&predict_result,))?
        } else {
            predict_result
        };
        
        // Drop the cancelable guard now that predict is done
        drop(_cancelable_guard);

        // Exit context manager (handles exceptions properly)
        let none = py.None();
        redirector.call_method1("__exit__", (none.bind(py), none.bind(py), none.bind(py)))?;

        // Log captured output
        if let Ok(guard) = logs.lock() {
            for (source, text) in guard.iter() {
                let text = text.trim_end();
                if !text.is_empty() {
                    // source is "<stdout>" or "<stderr>" from Python's stream.name
                    if source.contains("stdout") {
                        tracing::info!(target: "predict.stdout", "{}", text);
                    } else {
                        tracing::warn!(target: "predict.stderr", "{}", text);
                    }
                }
            }
        }

        Ok(result.unbind())
    }

    /// Call predict() with JSON input, returning a PredictionResult.
    ///
    /// This handles the full conversion from JSON -> Python dict -> predict -> JSON.
    /// Detects generator returns and iterates them to collect all outputs.
    ///
    /// The input is processed through the runtime-specific input processor:
    /// - Pydantic: validates through BaseInput, downloads URLPath files
    /// - Coglet: uses ADT-based validation
    pub fn predict(&self, input: serde_json::Value) -> Result<PredictionResult, PredictionError> {
        Python::attach(|py| {
            // Convert JSON input to Python dict via JSON serialization
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;
            let types_module = py.import("types").map_err(|e| {
                PredictionError::Failed(format!("Failed to import types module: {}", e))
            })?;
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;

            // Convert serde_json::Value to Python object via JSON string
            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;

            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            // Ensure input is a dict (or convert if needed)
            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            // Process input through the runtime-specific processor
            // This handles validation, type coercion, and file downloads
            let input_dict = self.input_processor.prepare(py, raw_input_dict).map_err(|e| {
                PredictionError::InvalidInput(format!("Input validation failed: {}", e))
            })?;

            // Call predict - check for CancelationException
            let result = self.predict_raw(py, &input_dict).map_err(|e| {
                // Check if this is a CancelationException
                if is_cancelation_exception(py, &e) {
                    return PredictionError::Cancelled;
                }
                PredictionError::Failed(format!("Prediction failed: {}", e))
            })?;

            let result_bound = result.bind(py);

            // Check if result is a generator
            let is_generator: bool = result_bound.is_instance(&generator_type).unwrap_or(false);

            let output = if is_generator {
                // Iterate generator and collect all outputs
                tracing::debug!("Detected generator output, iterating...");
                let mut outputs = Vec::new();
                let iter = result_bound.try_iter().map_err(|e| {
                    PredictionError::Failed(format!("Failed to iterate generator: {}", e))
                })?;

                for item in iter {
                    let item = item.map_err(|e| {
                        // Check if generator was cancelled
                        if is_cancelation_exception(py, &e) {
                            return PredictionError::Cancelled;
                        }
                        PredictionError::Failed(format!("Generator iteration error: {}", e))
                    })?;

                    // Process output (handles Path -> base64, Pydantic models, etc.)
                    let processed = output::process_output_item(py, &item).map_err(|e| {
                        PredictionError::Failed(format!("Failed to process output item: {}", e))
                    })?;

                    // Convert to JSON
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

                PredictionOutput::Stream(outputs)
            } else {
                // Process output (handles Path -> base64, Pydantic models, etc.)
                let processed = output::process_output(py, result_bound, None).map_err(|e| {
                    PredictionError::Failed(format!("Failed to process output: {}", e))
                })?;

                // Single value output
                let result_str: String = json_module
                    .call_method1("dumps", (&processed,))
                    .map_err(|e| {
                        PredictionError::Failed(format!("Failed to serialize output: {}", e))
                    })?
                    .extract()
                    .map_err(|e| {
                        PredictionError::Failed(format!("Failed to extract output string: {}", e))
                    })?;

                let output_json: serde_json::Value = serde_json::from_str(&result_str).map_err(|e| {
                    PredictionError::Failed(format!("Failed to parse output JSON: {}", e))
                })?;

                PredictionOutput::Single(output_json)
            };

            Ok(PredictionResult { output, predict_time: None })
        })
    }

    /// Async version of predict for async predictors.
    ///
    /// Uses pyo3-async-runtimes to convert Python coroutine to Rust future,
    /// enabling true concurrency via tokio. The Python asyncio event loop
    /// runs in a dedicated background thread.
    ///
    /// For async generators, we wrap the iteration in a Python coroutine that
    /// collects all yielded values into a list.
    pub async fn predict_async(
        &self,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        let is_async_gen = self.is_async_gen;
        
        // Prepare coroutine and schedule on the shared event loop
        let (rust_future, json_module) = Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;

            // Get or initialize the shared event loop (runs in background thread)
            let event_loop = get_or_init_event_loop(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get event loop: {}", e))
            })?;

            // Create TaskLocals with the shared event loop
            let locals = pyo3_async_runtimes::TaskLocals::new(event_loop.bind(py).clone());

            // Convert input to Python dict
            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;
            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            // Process input through the runtime-specific processor
            // This handles validation, type coercion, and file downloads
            let input_dict = self.input_processor.prepare(py, raw_input_dict).map_err(|e| {
                PredictionError::InvalidInput(format!("Input validation failed: {}", e))
            })?;

            // Call predict - returns coroutine or async generator
            let instance = self.instance.bind(py);
            let predict_result = instance
                .call_method("predict", (), Some(&input_dict))
                .map_err(|e| PredictionError::Failed(format!("Failed to call predict: {}", e)))?;

            // For async generators, wrap in a coroutine that collects all values
            let awaitable = if is_async_gen {
                // Create a Python coroutine that iterates the async generator
                // and collects all values into a list using exec()
                let collect_code = "
async def _collect_async_gen(agen):
    results = []
    async for item in agen:
        results.append(item)
    return results
";
                // Execute the helper function definition using exec()
                let builtins = py.import("builtins").map_err(|e| {
                    PredictionError::Failed(format!("Failed to import builtins: {}", e))
                })?;
                let exec_fn = builtins.getattr("exec").map_err(|e| {
                    PredictionError::Failed(format!("Failed to get exec: {}", e))
                })?;
                
                let globals = PyDict::new(py);
                exec_fn.call1((collect_code, &globals)).map_err(|e| {
                    PredictionError::Failed(format!("Failed to define collect helper: {}", e))
                })?;
                
                let collect_fn = globals.get_item("_collect_async_gen").map_err(|e| {
                    PredictionError::Failed(format!("Failed to get collect helper: {}", e))
                })?.ok_or_else(|| {
                    PredictionError::Failed("_collect_async_gen not found".to_string())
                })?;
                
                // Call _collect_async_gen(async_generator) to get a coroutine
                collect_fn.call1((&predict_result,)).map_err(|e| {
                    PredictionError::Failed(format!("Failed to wrap async generator: {}", e))
                })?
            } else {
                predict_result
            };

            // Convert coroutine to Rust future - schedules on the shared event loop
            let rust_future = pyo3_async_runtimes::into_future_with_locals(&locals, awaitable)
                .map_err(|e| PredictionError::Failed(format!("Failed to convert coroutine: {}", e)))?;

            Ok::<_, PredictionError>((rust_future, json_module.unbind()))
        })?;

        // Await the Rust future (this is where concurrency happens!)
        // The Python coroutine runs on the background asyncio event loop
        let py_result = rust_future.await.map_err(|e| {
            // Check for asyncio.CancelledError in the error chain
            let err_str = e.to_string();
            if err_str.contains("CancelledError") || err_str.contains("Cancelation") {
                return PredictionError::Cancelled;
            }
            PredictionError::Failed(format!("Async prediction failed: {}", e))
        })?;

        // Convert result to JSON
        Python::attach(|py| {
            let json_module = json_module.bind(py);
            let result_bound = py_result.bind(py);

            // For async generators, the result is a list - process each item
            // For regular async, process the single value
            let output = if is_async_gen {
                // result_bound is a list from _collect_async_gen
                let mut outputs = Vec::new();
                if let Ok(list) = result_bound.extract::<Vec<Bound<'_, PyAny>>>() {
                    for item in list {
                        // Process each output item (handles Path -> base64, etc.)
                        let processed = output::process_output_item(py, &item).map_err(|e| {
                            PredictionError::Failed(format!("Failed to process output item: {}", e))
                        })?;

                        let item_str: String = json_module
                            .call_method1("dumps", (&processed,))
                            .map_err(|e| PredictionError::Failed(format!("Failed to serialize output item: {}", e)))?
                            .extract()
                            .map_err(|e| PredictionError::Failed(format!("Failed to extract output item: {}", e)))?;

                        let item_json: serde_json::Value = serde_json::from_str(&item_str)
                            .map_err(|e| PredictionError::Failed(format!("Failed to parse output JSON: {}", e)))?;

                        outputs.push(item_json);
                    }
                }
                PredictionOutput::Stream(outputs)
            } else {
                // Process output (handles Path -> base64, Pydantic models, etc.)
                let processed = output::process_output(py, result_bound, None).map_err(|e| {
                    PredictionError::Failed(format!("Failed to process output: {}", e))
                })?;

                let result_str: String = json_module
                    .call_method1("dumps", (&processed,))
                    .map_err(|e| PredictionError::Failed(format!("Failed to serialize output: {}", e)))?
                    .extract()
                    .map_err(|e| PredictionError::Failed(format!("Failed to extract output: {}", e)))?;

                let output_json: serde_json::Value = serde_json::from_str(&result_str)
                    .map_err(|e| PredictionError::Failed(format!("Failed to parse output JSON: {}", e)))?;

                PredictionOutput::Single(output_json)
            };

            Ok(PredictionResult {
                output,
                predict_time: None,
            })
        })
    }
}
