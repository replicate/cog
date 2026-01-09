//! Python predictor loading and invocation.

use pyo3::prelude::*;
use pyo3::types::PyDict;

/// A loaded Python predictor instance.
pub struct PythonPredictor {
    instance: PyObject,
}

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
    pub fn predict(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PyObject> {
        let instance = self.instance.bind(py);
        let result = instance.call_method("predict", (), Some(input))?;
        Ok(result.unbind())
    }
}
