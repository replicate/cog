//! Bridge between coglet-worker's PredictHandler trait and PythonPredictor.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use pyo3::prelude::*;

use coglet_worker::{PredictHandler, PredictResult, SlotId, SlotSender};

use crate::predictor::PythonPredictor;

/// Per-slot state for cancellation and tracking.
struct SlotState {
    /// Whether this slot has been cancelled.
    cancelled: bool,
    /// Whether a prediction is currently in progress on this slot.
    in_progress: bool,
    /// Whether the current prediction is async (for cancel routing).
    is_async: bool,
}

impl Default for SlotState {
    fn default() -> Self {
        Self {
            cancelled: false,
            in_progress: false,
            is_async: false,
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
    /// Per-slot cancellation state (keyed by SlotId).
    slots: Mutex<HashMap<SlotId, SlotState>>,
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
    fn take_cancelled(&self, slot: SlotId) -> bool {
        let mut slots = self.slots.lock().unwrap();
        let state = slots.entry(slot).or_default();
        let was_cancelled = state.cancelled;
        state.cancelled = false;
        was_cancelled
    }

    /// Mark a slot as having a prediction in progress.
    fn start_prediction(&self, slot: SlotId, is_async: bool) {
        let mut slots = self.slots.lock().unwrap();
        let state = slots.entry(slot).or_default();
        state.cancelled = false;
        state.in_progress = true;
        state.is_async = is_async;
    }

    /// Clear prediction state for a slot.
    fn finish_prediction(&self, slot: SlotId) {
        let mut slots = self.slots.lock().unwrap();
        if let Some(state) = slots.get_mut(&slot) {
            state.in_progress = false;
            state.is_async = false;
        }
        
        // Also unregister from async task registry (no-op if not async)
        crate::async_runtime::unregister_task(slot);
    }
    
    /// Check if a slot's prediction is async.
    fn is_slot_async(&self, slot: SlotId) -> bool {
        let slots = self.slots.lock().unwrap();
        slots.get(&slot).map(|s| s.is_async).unwrap_or(false)
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
        slot: SlotId,
        id: String,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> PredictResult {
        tracing::debug!(%slot, %id, "PythonPredictHandler::predict starting");
        
        // Get predictor and determine if async
        let (pred, is_async) = {
            let guard = self.predictor.lock().unwrap();
            match guard.as_ref() {
                Some(p) => (Arc::clone(p), p.is_async()),
                None => {
                    return PredictResult::failed(
                        "Predictor not initialized".to_string(),
                        0.0,
                    );
                }
            }
        };
        tracing::debug!(%slot, %id, is_async, "Got predictor");

        // Track that we're starting a prediction on this slot
        self.start_prediction(slot, is_async);

        // Check cancellation first (in case cancel was called before we started)
        if self.take_cancelled(slot) {
            self.finish_prediction(slot);
            return PredictResult::cancelled(0.0);
        }

        // Enter prediction context - sets cog_prediction_id ContextVar for log routing
        tracing::debug!(%slot, %id, "Entering prediction context");
        let prediction_id = id.clone();
        let slot_sender_clone = slot_sender.clone();
        let log_guard = Python::attach(|py| {
            tracing::debug!(%slot, %id, "Got GIL, calling PredictionLogGuard::enter");
            crate::log_writer::PredictionLogGuard::enter(py, prediction_id.clone(), slot_sender_clone)
        });
        let log_guard = match log_guard {
            Ok(g) => Some(g),
            Err(e) => {
                tracing::warn!(error = %e, "Failed to enter prediction context");
                None
            }
        };
        tracing::debug!(%slot, %id, "Prediction context entered");

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
                // Sync train - wrap in cancelable guard for SIGUSR1 handling
                let _cancelable = crate::cancel::enter_cancelable();
                pred.train_worker(input)
            }
        } else {
            // Prediction mode
            tracing::debug!(%slot, %id, is_async = pred.is_async(), "Running prediction");
            if pred.is_async() {
                pred.predict_async_worker(input)
            } else {
                // Sync predict - wrap in cancelable guard for SIGUSR1 handling
                let _cancelable = crate::cancel::enter_cancelable();
                tracing::debug!(%slot, %id, "Calling predict_worker");
                let r = pred.predict_worker(input);
                tracing::debug!(%slot, %id, "predict_worker returned");
                r
            }
        };
        tracing::debug!(%slot, %id, "Prediction completed");

        self.finish_prediction(slot);
        
        // Exit prediction context - unregisters prediction from routing
        // (ContextVar reset is automatic when task ends for async)
        drop(log_guard);

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

    fn cancel(&self, slot: SlotId) {
        let is_async = self.is_slot_async(slot);
        
        // Set cancelled flag (checked by sync predictors between operations)
        {
            let mut slots = self.slots.lock().unwrap();
            let state = slots.entry(slot).or_default();
            state.cancelled = true;
        }
        
        if is_async {
            // Async: cancel the Python asyncio task
            Python::attach(|py| {
                if let Err(e) = crate::async_runtime::cancel_task(py, slot) {
                    tracing::warn!(%slot, error = %e, "Failed to cancel async task");
                }
            });
        } else {
            // Sync: send SIGUSR1 to interrupt blocking Python code
            if let Err(e) = crate::cancel::send_cancel_signal() {
                tracing::warn!(%slot, error = %e, "Failed to send cancel signal");
            }
        }
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
