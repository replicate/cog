//! Python predictor loading and invocation.

use pyo3::prelude::*;
use pyo3::types::PyDict;

use coglet_core::{PredictionError, PredictionOutput, PredictionResult};

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
/// ## Async Predictors (future work)
/// - `async def predict()` allows Python to manage concurrency
/// - Python's asyncio handles yielding during I/O
/// - Can support max_concurrency > 1 safely
///
/// # Current Implementation
///
/// We use a single prediction slot (max_concurrency=1). The predict call holds
/// the GIL for JSON conversion but Python/torch can release it during compute.
pub struct PythonPredictor {
    instance: PyObject,
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

        Ok(Self { instance })
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

        let predict_result = instance.call_method("predict", (), Some(input));

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

        predict_result.map(|r| r.unbind())
    }

    /// Call predict() with JSON input, returning a PredictionResult.
    ///
    /// This handles the full conversion from JSON -> Python dict -> predict -> JSON.
    /// Detects generator returns and iterates them to collect all outputs.
    pub fn predict(&self, input: serde_json::Value) -> Result<PredictionResult, PredictionError> {
        Python::with_gil(|py| {
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
            let input_dict: &Bound<'_, PyDict> = py_input.downcast().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            // Call predict
            let result = self.predict_raw(py, input_dict).map_err(|e| {
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
                        PredictionError::Failed(format!("Generator iteration error: {}", e))
                    })?;

                    // Convert each item to JSON
                    let item_str: String = json_module
                        .call_method1("dumps", (&item,))
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
                // Single value output
                let result_str: String = json_module
                    .call_method1("dumps", (result_bound,))
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
}
