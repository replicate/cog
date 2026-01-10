//! Bridge between coglet-worker's PredictHandler trait and PythonPredictor.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use pyo3::prelude::*;

use coglet_worker::{PredictHandler, PredictResult};

use crate::predictor::PythonPredictor;

/// Wraps PythonPredictor to implement the PredictHandler trait.
pub struct PythonPredictHandler {
    predictor_ref: String,
    predictor: std::sync::Mutex<Option<Arc<PythonPredictor>>>,
    cancelled: AtomicBool,
}

impl PythonPredictHandler {
    pub fn new(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            predictor: std::sync::Mutex::new(None),
            cancelled: AtomicBool::new(false),
        }
    }
}

#[async_trait::async_trait]
impl PredictHandler for PythonPredictHandler {
    async fn setup(&self) -> Result<(), String> {
        // Load and setup predictor
        // This runs in the worker subprocess, so we own Python
        Python::attach(|py| {
            tracing::info!(predictor_ref = %self.predictor_ref, "Loading predictor");
            
            let pred = PythonPredictor::load(py, &self.predictor_ref)
                .map_err(|e| format!("Failed to load predictor: {}", e))?;
            
            tracing::info!("Running setup");
            pred.setup(py)
                .map_err(|e| format!("Setup failed: {}", e))?;
            
            // Store predictor for later use
            let mut guard = self.predictor.lock().unwrap();
            *guard = Some(Arc::new(pred));
            
            tracing::info!("Setup complete");
            Ok(())
        })
    }

    async fn predict(&self, input: serde_json::Value) -> PredictResult {
        // Check cancellation first
        if self.cancelled.swap(false, Ordering::SeqCst) {
            return PredictResult::cancelled(String::new(), 0.0);
        }

        let pred = {
            let guard = self.predictor.lock().unwrap();
            match guard.as_ref() {
                Some(p) => Arc::clone(p),
                None => {
                    return PredictResult::failed(
                        "Predictor not initialized".to_string(),
                        String::new(),
                        0.0,
                    );
                }
            }
        };

        // Run prediction
        let start = std::time::Instant::now();
        
        // Check if async or sync predictor
        let result = if pred.is_async() {
            // Async prediction
            match pred.predict_async(input).await {
                Ok(r) => PredictResult::success(
                    output_to_json(r.output),
                    String::new(),
                    start.elapsed().as_secs_f64(),
                ),
                Err(e) => {
                    if matches!(e, coglet_core::PredictionError::Cancelled) {
                        PredictResult::cancelled(String::new(), start.elapsed().as_secs_f64())
                    } else {
                        PredictResult::failed(
                            e.to_string(),
                            String::new(),
                            start.elapsed().as_secs_f64(),
                        )
                    }
                }
            }
        } else {
            // Sync prediction - use predict_worker to avoid stdout redirection
            match pred.predict_worker(input) {
                Ok(r) => PredictResult::success(
                    output_to_json(r.output),
                    String::new(),
                    start.elapsed().as_secs_f64(),
                ),
                Err(e) => {
                    if matches!(e, coglet_core::PredictionError::Cancelled) {
                        PredictResult::cancelled(String::new(), start.elapsed().as_secs_f64())
                    } else {
                        PredictResult::failed(
                            e.to_string(),
                            String::new(),
                            start.elapsed().as_secs_f64(),
                        )
                    }
                }
            }
        };

        result
    }

    fn cancel(&self) {
        self.cancelled.store(true, Ordering::SeqCst);
        // TODO: Also send SIGUSR1 for sync predictors?
    }
}

/// Convert PredictionOutput to serde_json::Value
fn output_to_json(output: coglet_core::PredictionOutput) -> serde_json::Value {
    match output {
        coglet_core::PredictionOutput::Single(v) => v,
        coglet_core::PredictionOutput::Stream(v) => serde_json::Value::Array(v),
    }
}
