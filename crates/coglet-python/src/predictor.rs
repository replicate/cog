//! Python predictor wrapping.
//!
//! Loads Python predictor classes and wraps them for Rust.

use std::sync::Arc;
use std::time::Instant;

use pyo3::prelude::*;

use coglet_core::bridge::protocol::SlotId;
use coglet_core::worker::{PredictResult, SlotSender};

use crate::cancel;

/// Wrapper for Python predictor class.
pub struct PythonPredictor {
    pub predictor_ref: String,
    pub is_train: bool,
    pub predictor: Option<PyObject>,
    pub schema: Option<serde_json::Value>,
}

impl PythonPredictor {
    pub fn new(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            is_train: false,
            predictor: None,
            schema: None,
        }
    }

    pub fn new_train(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            is_train: true,
            predictor: None,
            schema: None,
        }
    }

    /// Load the predictor class from the module path.
    pub fn load(&mut self) -> Result<(), String> {
        Python::with_gil(|py| {
            let (module_path, class_name) = parse_predictor_ref(&self.predictor_ref)?;

            // Import the module
            let module = py.import(module_path.as_str())
                .map_err(|e| format!("Failed to import {}: {}", module_path, e))?;

            // Get the class
            let class = module.getattr(class_name.as_str())
                .map_err(|e| format!("Failed to get class {}: {}", class_name, e))?;

            // Instantiate the predictor
            let instance = class.call0()
                .map_err(|e| format!("Failed to instantiate {}: {}", class_name, e))?;

            self.predictor = Some(instance.into());

            // Try to get schema
            if let Ok(cog_types) = py.import("cog.types")
                && let Ok(get_schema) = cog_types.getattr("get_input_schema")
                && let Ok(schema_dict) = get_schema.call1((class,))
                && let Ok(json_str) = py.import("json")
                    .and_then(|json| json.call_method1("dumps", (schema_dict,)))
                    .and_then(|s| s.extract::<String>())
            {
                self.schema = serde_json::from_str(&json_str).ok();
            }

            Ok(())
        })
    }

    /// Run setup on the predictor.
    pub fn setup(&self) -> Result<(), String> {
        let predictor = self.predictor.as_ref()
            .ok_or_else(|| "Predictor not loaded".to_string())?;

        Python::with_gil(|py| {
            let predictor = predictor.bind(py);

            // Check if setup method exists
            if predictor.hasattr("setup").map_err(|e| e.to_string())? {
                predictor.call_method0("setup")
                    .map_err(|e| format!("setup() failed: {}", e))?;
            }

            Ok(())
        })
    }

    /// Run a prediction.
    pub fn predict(
        &self,
        _slot: SlotId,
        id: String,
        input: serde_json::Value,
        _slot_sender: Arc<SlotSender>,
    ) -> PredictResult {
        let start = Instant::now();

        let predictor = match self.predictor.as_ref() {
            Some(p) => p,
            None => return PredictResult::failed("Predictor not loaded".to_string(), 0.0),
        };

        let method = if self.is_train { "train" } else { "predict" };

        let result = Python::with_gil(|py| -> Result<serde_json::Value, String> {
            let predictor = predictor.bind(py);

            // Convert input to Python dict
            let input_str = serde_json::to_string(&input)
                .map_err(|e| format!("Failed to serialize input: {}", e))?;
            let json_module = py.import("json").map_err(|e| e.to_string())?;
            let input_dict = json_module.call_method1("loads", (input_str,))
                .map_err(|e| format!("Failed to parse input: {}", e))?;

            // Mark as cancelable during predict
            cancel::set_cancelable(true);

            let output = predictor.call_method1(method, (input_dict,))
                .map_err(|e| {
                    if e.is_instance_of::<pyo3::exceptions::PyKeyboardInterrupt>(py) {
                        "Cancelled".to_string()
                    } else {
                        format!("{}() failed: {}", method, e)
                    }
                })?;

            cancel::set_cancelable(false);

            // Convert output to JSON
            let output_str = json_module.call_method1("dumps", (output,))
                .map_err(|e| format!("Failed to serialize output: {}", e))?
                .extract::<String>()
                .map_err(|e| e.to_string())?;

            serde_json::from_str(&output_str)
                .map_err(|e| format!("Invalid output JSON: {}", e))
        });

        let predict_time = start.elapsed().as_secs_f64();

        match result {
            Ok(output) => {
                tracing::info!(prediction_id = %id, predict_time, "Prediction succeeded");
                PredictResult::success(output, predict_time)
            }
            Err(e) if e == "Cancelled" => {
                tracing::info!(prediction_id = %id, "Prediction cancelled");
                PredictResult::cancelled(predict_time)
            }
            Err(e) => {
                tracing::error!(prediction_id = %id, error = %e, "Prediction failed");
                PredictResult::failed(e, predict_time)
            }
        }
    }

    pub fn schema(&self) -> Option<serde_json::Value> {
        self.schema.clone()
    }
}

fn parse_predictor_ref(predictor_ref: &str) -> Result<(String, String), String> {
    let parts: Vec<&str> = predictor_ref.rsplitn(2, ':').collect();
    if parts.len() != 2 {
        return Err(format!(
            "Invalid predictor ref '{}': expected 'module.path:ClassName'",
            predictor_ref
        ));
    }

    let class_name = parts[0].to_string();
    let module_path = parts[1].replace('/', ".").trim_end_matches(".py").to_string();

    Ok((module_path, class_name))
}
