//! Python predictor loading and invocation.

use pyo3::prelude::*;
use pyo3::types::PyDict;

use coglet_core::{PredictionError, PredictionResult};

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

    /// Call setup() on the predictor.
    pub fn setup(&self, py: Python<'_>) -> PyResult<()> {
        let instance = self.instance.bind(py);
        // Check if setup method exists and call it
        if instance.hasattr("setup")? {
            instance.call_method0("setup")?;
        }
        Ok(())
    }

    /// Call predict() with the given input dict, returning JSON-serializable output.
    pub fn predict_raw(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PyObject> {
        let instance = self.instance.bind(py);
        let result = instance.call_method("predict", (), Some(input))?;
        Ok(result.unbind())
    }

    /// Call predict() with JSON input, returning a PredictionResult.
    ///
    /// This handles the full conversion from JSON -> Python dict -> predict -> JSON.
    pub fn predict(&self, input: serde_json::Value) -> Result<PredictionResult, PredictionError> {
        Python::with_gil(|py| {
            // Convert JSON input to Python dict via JSON serialization
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;

            // Convert serde_json::Value to Python object via JSON string
            // (more reliable than pythonize for complex types)
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

            // Convert Python result back to JSON
            let result_bound = result.bind(py);
            let result_str: String = json_module
                .call_method1("dumps", (result_bound,))
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to serialize output: {}", e))
                })?
                .extract()
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to extract output string: {}", e))
                })?;

            let output: serde_json::Value = serde_json::from_str(&result_str).map_err(|e| {
                PredictionError::Failed(format!("Failed to parse output JSON: {}", e))
            })?;

            Ok(PredictionResult { output })
        })
    }
}
