//! Bridge between coglet-worker's PredictHandler trait and PythonPredictor.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use pyo3::prelude::*;

use coglet_worker::{PredictHandler, PredictResult};

use crate::predictor::PythonPredictor;

/// Wraps PythonPredictor to implement the PredictHandler trait.
/// 
/// The `is_train` flag determines whether predict() calls the Python
/// predict() or train() method. This is set at construction time.
/// 
/// BUG-FOR-BUG COMPATIBILITY: In cog mainline, training routes use a worker
/// that was created with is_train=false, so training routes actually call
/// predict() instead of train(). We replicate this by always creating the
/// handler with is_train=false. To fix this bug, pass is_train=true when
/// creating a handler for training routes.
pub struct PythonPredictHandler {
    predictor_ref: String,
    predictor: std::sync::Mutex<Option<Arc<PythonPredictor>>>,
    cancelled: AtomicBool,
    /// If true, predict() calls train() instead of predict().
    /// BUG: cog mainline always sets this to false, even for training routes.
    is_train: bool,
}

impl PythonPredictHandler {
    /// Create a handler in prediction mode.
    pub fn new(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            predictor: std::sync::Mutex::new(None),
            cancelled: AtomicBool::new(false),
            is_train: false,
        }
    }

    /// Create a handler in training mode.
    /// 
    /// NOTE: For bug-for-bug compatibility with cog mainline, use new() instead.
    /// Cog mainline's training routes incorrectly use a predict-mode worker.
    #[allow(dead_code)]
    pub fn new_train(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            predictor: std::sync::Mutex::new(None),
            cancelled: AtomicBool::new(false),
            is_train: true,
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

        // Run prediction or training based on is_train mode.
        // 
        // BUG-FOR-BUG: In cog mainline, is_train is set at worker creation time,
        // not per-request. Training routes use a worker created with is_train=false,
        // so they incorrectly call predict() instead of train(). We replicate this
        // by always creating handlers with is_train=false (see new()).
        let start = std::time::Instant::now();
        
        let result = if self.is_train {
            // Training mode - check if train() exists
            if !pred.has_train() {
                return PredictResult::failed(
                    "Training not supported by this predictor".to_string(),
                    String::new(),
                    0.0,
                );
            }
            // Use worker-mode train (no stdout redirection, sync execution)
            if pred.is_train_async() {
                pred.train_async_worker(input)
            } else {
                pred.train_worker(input)
            }
        } else {
            // Prediction mode
            if pred.is_async() {
                pred.predict_async_worker(input)
            } else {
                pred.predict_worker(input)
            }
        };

        match result {
            Ok(r) => PredictResult::success(
                output_to_json(r.output),
                r.logs,  // Pass captured logs through protocol
                start.elapsed().as_secs_f64(),
            ),
            Err(e) => {
                if matches!(e, coglet_core::PredictionError::Cancelled) {
                    PredictResult::cancelled(String::new(), start.elapsed().as_secs_f64())
                } else {
                    PredictResult::failed(
                        e.to_string(),
                        String::new(),  // Logs already included in error message
                        start.elapsed().as_secs_f64(),
                    )
                }
            }
        }
    }

    fn cancel(&self) {
        self.cancelled.store(true, Ordering::SeqCst);
        // TODO: Also send SIGUSR1 for sync predictors?
    }

    fn schema(&self) -> Option<serde_json::Value> {
        let guard = self.predictor.lock().unwrap();
        guard.as_ref().and_then(|pred| pred.schema())
    }
}

/// Convert PredictionOutput to serde_json::Value
fn output_to_json(output: coglet_core::PredictionOutput) -> serde_json::Value {
    match output {
        coglet_core::PredictionOutput::Single(v) => v,
        coglet_core::PredictionOutput::Stream(v) => serde_json::Value::Array(v),
    }
}
