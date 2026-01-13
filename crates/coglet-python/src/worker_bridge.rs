//! Bridge between coglet-worker's PredictHandler trait and PythonPredictor.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use pyo3::prelude::*;

use coglet_worker::{PredictHandler, PredictResult, SlotSender};

use crate::predictor::PythonPredictor;

/// Per-slot state for cancellation tracking.
struct SlotState {
    /// Whether this slot has been cancelled.
    cancelled: bool,
    /// Current prediction ID (for logging).
    prediction_id: Option<String>,
}

impl Default for SlotState {
    fn default() -> Self {
        Self {
            cancelled: false,
            prediction_id: None,
        }
    }
}

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
    predictor: Mutex<Option<Arc<PythonPredictor>>>,
    /// Per-slot cancellation state.
    slots: Mutex<HashMap<usize, SlotState>>,
    /// If true, predict() calls train() instead of predict().
    /// BUG: cog mainline always sets this to false, even for training routes.
    is_train: bool,
}

impl PythonPredictHandler {
    /// Create a handler in prediction mode.
    pub fn new(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            predictor: Mutex::new(None),
            slots: Mutex::new(HashMap::new()),
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
            predictor: Mutex::new(None),
            slots: Mutex::new(HashMap::new()),
            is_train: true,
        }
    }

    /// Check and clear the cancelled flag for a slot.
    fn take_cancelled(&self, slot: usize) -> bool {
        let mut slots = self.slots.lock().unwrap();
        let state = slots.entry(slot).or_default();
        let was_cancelled = state.cancelled;
        state.cancelled = false;
        was_cancelled
    }

    /// Mark a slot as having a prediction in progress.
    fn start_prediction(&self, slot: usize, id: &str) {
        let mut slots = self.slots.lock().unwrap();
        let state = slots.entry(slot).or_default();
        state.prediction_id = Some(id.to_string());
        state.cancelled = false;
    }

    /// Clear prediction state for a slot.
    fn finish_prediction(&self, slot: usize) {
        let mut slots = self.slots.lock().unwrap();
        if let Some(state) = slots.get_mut(&slot) {
            state.prediction_id = None;
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

    async fn predict(
        &self,
        slot: usize,
        id: String,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> PredictResult {
        // Track that we're starting a prediction on this slot
        self.start_prediction(slot, &id);

        // Check cancellation first (in case cancel was called before we started)
        if self.take_cancelled(slot) {
            self.finish_prediction(slot);
            return PredictResult::cancelled(0.0);
        }

        let pred = {
            let guard = self.predictor.lock().unwrap();
            match guard.as_ref() {
                Some(p) => Arc::clone(p),
                None => {
                    self.finish_prediction(slot);
                    return PredictResult::failed(
                        "Predictor not initialized".to_string(),
                        0.0,
                    );
                }
            }
        };

        // Install SlotLogGuard to capture stdout/stderr and stream to slot socket
        let _log_guard = Python::attach(|py| {
            crate::log_writer::SlotLogGuard::install(py, slot_sender)
        });

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
                self.finish_prediction(slot);
                return PredictResult::failed(
                    "Training not supported by this predictor".to_string(),
                    0.0,
                );
            }
            // Use worker-mode train
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

        self.finish_prediction(slot);
        // _log_guard dropped here, restores original stdout/stderr

        match result {
            Ok(r) => {
                // Logs already streamed via SlotLogWriter
                PredictResult::success(
                    output_to_json(r.output),
                    start.elapsed().as_secs_f64(),
                )
            }
            Err(e) => {
                if matches!(e, coglet_core::PredictionError::Cancelled) {
                    PredictResult::cancelled(start.elapsed().as_secs_f64())
                } else {
                    PredictResult::failed(
                        e.to_string(),
                        start.elapsed().as_secs_f64(),
                    )
                }
            }
        }
    }

    fn cancel(&self, slot: usize) {
        let mut slots = self.slots.lock().unwrap();
        let state = slots.entry(slot).or_default();
        state.cancelled = true;
        // TODO: Also send SIGUSR1 for sync predictors?
        tracing::debug!(slot, "Cancellation requested");
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
