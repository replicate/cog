//! Bridge between coglet-worker's PredictHandler trait and PythonPredictor.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::thread::JoinHandle;

use pyo3::prelude::*;

use coglet_core::bridge::protocol::SlotId;
use coglet_core::worker::{PredictHandler, PredictResult, SetupError, SlotSender};

use crate::predictor::PythonPredictor;

/// What operation the handler performs
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HandlerMode {
    /// Calls predict() method
    Predict,
    /// Calls train() method
    Train,
}

/// SDK implementation type detected from the Python predictor.
///
/// This enum allows for future extensibility if additional SDK
/// implementations are needed (e.g., Node.js).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SdkImplementation {
    /// Standard cog Python SDK
    Cog,
    /// Unable to detect SDK type
    Unknown,
}

impl std::fmt::Display for SdkImplementation {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Cog => write!(f, "cog"),
            Self::Unknown => write!(f, "unknown"),
        }
    }
}

/// Current state of a prediction slot
#[derive(Debug, Default)]
pub enum SlotState {
    /// No prediction running
    #[default]
    Idle,
    /// Sync prediction in progress
    SyncPrediction {
        cancelled: bool,
        /// Python thread identifier (for `PyThreadState_SetAsyncExc`)
        py_thread_id: std::ffi::c_long,
    },
    /// Async prediction in progress
    AsyncPrediction {
        /// Future for cancellation
        future: Py<PyAny>,
        cancelled: bool,
    },
}

impl SlotState {
    pub fn is_cancelled(&self) -> bool {
        match self {
            SlotState::SyncPrediction { cancelled, .. } => *cancelled,
            SlotState::AsyncPrediction { cancelled, .. } => *cancelled,
            SlotState::Idle => false,
        }
    }

    pub fn mark_cancelled(&mut self) {
        match self {
            SlotState::SyncPrediction { cancelled, .. } => *cancelled = true,
            SlotState::AsyncPrediction { cancelled, .. } => *cancelled = true,
            SlotState::Idle => { /* no-op */ }
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
    /// What operation this handler performs (predict or train).
    /// BUG: cog mainline always uses Predict mode, even for training routes.
    mode: HandlerMode,
    /// Shared asyncio event loop for async predictions (runs in dedicated thread).
    async_loop: Mutex<Option<Py<PyAny>>>,
    /// Handle to the asyncio loop thread for joining on shutdown.
    async_thread: Mutex<Option<JoinHandle<()>>>,
}

impl PythonPredictHandler {
    /// Create a handler in prediction mode.
    pub fn new(predictor_ref: String) -> Result<Self, SetupError> {
        let (loop_obj, thread) = Self::init_async_loop()?;
        Ok(Self {
            predictor_ref,
            predictor: Mutex::new(None),
            slots: Mutex::new(HashMap::new()),
            mode: HandlerMode::Predict,
            async_loop: Mutex::new(Some(loop_obj)),
            async_thread: Mutex::new(Some(thread)),
        })
    }

    /// Create a handler in training mode.
    ///
    /// NOTE: For bug-for-bug compatibility with cog mainline, use new() instead.
    /// Cog mainline's training routes incorrectly use a predict-mode worker.
    #[allow(dead_code)]
    pub fn new_train(predictor_ref: String) -> Result<Self, SetupError> {
        let (loop_obj, thread) = Self::init_async_loop()?;
        Ok(Self {
            predictor_ref,
            predictor: Mutex::new(None),
            slots: Mutex::new(HashMap::new()),
            mode: HandlerMode::Train,
            async_loop: Mutex::new(Some(loop_obj)),
            async_thread: Mutex::new(Some(thread)),
        })
    }

    /// Initialize the shared asyncio event loop in a dedicated thread.
    fn init_async_loop() -> Result<(Py<PyAny>, JoinHandle<()>), SetupError> {
        Python::attach(|py| {
            let asyncio = py
                .import("asyncio")
                .map_err(|e| SetupError::internal(format!("Failed to import asyncio: {}", e)))?;
            let loop_obj = asyncio
                .call_method0("new_event_loop")
                .map_err(|e| SetupError::internal(format!("Failed to create event loop: {}", e)))?;

            // Clone for the thread
            let loop_for_thread = loop_obj.clone().unbind();
            let loop_result = loop_obj.unbind();

            // Spawn thread running loop.run_forever()
            let thread = std::thread::spawn(move || {
                Python::attach(|py| {
                    let loop_ref = loop_for_thread.bind(py);
                    // These errors in the thread are logged but can't be propagated
                    // The thread dying will cause async predictions to fail
                    let Ok(asyncio) = py.import("asyncio") else {
                        tracing::error!("Failed to import asyncio in loop thread");
                        return;
                    };
                    if let Err(e) = asyncio.call_method1("set_event_loop", (loop_ref,)) {
                        tracing::error!(error = %e, "Failed to set event loop");
                        return;
                    }
                    tracing::trace!("Asyncio event loop thread starting");
                    if let Err(e) = loop_ref.call_method0("run_forever") {
                        tracing::error!(error = %e, "Asyncio event loop error");
                    }
                    tracing::trace!("Asyncio event loop thread exiting");
                });
            });

            Ok((loop_result, thread))
        })
    }

    // NOTE: All mutex locks in this file use .expect().
    // See log_writer.rs for the full rationale. Short version: poisoned mutex
    // means slot isolation is compromised. The panic hook installed by
    // coglet_core::worker sends a Fatal IPC message and aborts.

    /// Check the cancelled flag for a slot without clearing it.
    fn is_cancelled(&self, slot: SlotId) -> bool {
        let slots = self.slots.lock().expect("slots mutex poisoned");
        slots.get(&slot).is_some_and(|s| s.is_cancelled())
    }

    /// Check and clear the cancelled flag for a slot.
    fn take_cancelled(&self, slot: SlotId) -> bool {
        let mut slots = self.slots.lock().expect("slots mutex poisoned");
        let state = slots.entry(slot).or_default();
        let was_cancelled = state.is_cancelled();
        // Reset to idle after checking cancellation
        if was_cancelled {
            *state = SlotState::Idle;
        }
        was_cancelled
    }

    /// Mark a slot as having a sync prediction in progress.
    ///
    /// `py_thread_id` is the Python thread identifier of the thread that will
    /// run the prediction, for use with `PyThreadState_SetAsyncExc` on cancel.
    fn start_sync_prediction(&self, slot: SlotId, py_thread_id: std::ffi::c_long) {
        let mut slots = self.slots.lock().expect("slots mutex poisoned");
        slots.insert(
            slot,
            SlotState::SyncPrediction {
                cancelled: false,
                py_thread_id,
            },
        );
    }

    /// Mark a slot as having an async prediction in progress.
    fn start_async_prediction(&self, slot: SlotId, future: Py<PyAny>) {
        let mut slots = self.slots.lock().expect("slots mutex poisoned");
        slots.insert(
            slot,
            SlotState::AsyncPrediction {
                future,
                cancelled: false,
            },
        );
    }

    /// Clear prediction state for a slot.
    fn finish_prediction(&self, slot: SlotId) {
        let mut slots = self.slots.lock().expect("slots mutex poisoned");
        slots.insert(slot, SlotState::Idle);
    }

    /// Cancel an async prediction using future.cancel().
    /// Returns true if cancellation was requested, false if no future found.
    fn cancel_async_future(&self, slot: SlotId) -> bool {
        Python::attach(|py| {
            let future = {
                let slots = self.slots.lock().expect("slots mutex poisoned");
                if let Some(SlotState::AsyncPrediction { future, .. }) = slots.get(&slot) {
                    Some(future.clone_ref(py))
                } else {
                    None
                }
            };

            if let Some(future) = future {
                match future.call_method0(py, "cancel") {
                    Ok(_) => {
                        tracing::trace!(%slot, "Cancelled async future");
                        true
                    }
                    Err(e) => {
                        tracing::warn!(%slot, error = %e, "Failed to cancel async future");
                        false
                    }
                }
            } else {
                tracing::trace!(%slot, "No async future to cancel");
                false
            }
        })
    }

    /// Get a reference to the shared asyncio event loop.
    fn get_async_loop(&self) -> Option<Py<PyAny>> {
        Python::attach(|py| {
            self.async_loop
                .lock()
                .expect("async_loop mutex poisoned")
                .as_ref()
                .map(|l| l.clone_ref(py))
        })
    }
}

#[async_trait::async_trait]
impl PredictHandler for PythonPredictHandler {
    async fn setup(&self) -> Result<(), SetupError> {
        Python::attach(|py| {
            tracing::info!(predictor_ref = %self.predictor_ref, "Loading predictor");

            let pred = PythonPredictor::load(py, &self.predictor_ref)
                .map_err(|e| SetupError::load(e.to_string()))?;

            // Detect SDK implementation
            let sdk_impl = match py.import("cog._adt") {
                Ok(_) => SdkImplementation::Cog,
                Err(_) => SdkImplementation::Unknown,
            };
            tracing::info!(sdk_implementation = %sdk_impl, "Detected Cog SDK implementation");

            tracing::info!("Running setup");
            pred.setup(py)
                .map_err(|e| SetupError::setup(e.to_string()))?;

            let mut guard = self.predictor.lock().expect("predictor mutex poisoned");
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
        tracing::trace!(%slot, %id, "PythonPredictHandler::predict starting");

        // Get predictor
        let pred = {
            let guard = self.predictor.lock().expect("predictor mutex poisoned");
            match guard.as_ref() {
                Some(p) => Arc::clone(p),
                None => {
                    return PredictResult::failed("Predictor not initialized".to_string(), 0.0);
                }
            }
        };
        let is_async = pred.is_async();
        tracing::trace!(%slot, %id, is_async, "Got predictor");

        // Track that we're starting a prediction on this slot.
        // Capture the Python thread ID for this thread (used by
        // PyThreadState_SetAsyncExc to inject CancelationException on cancel).
        // For async predictions, the slot state is updated later with the future.
        let py_thread_id = crate::cancel::current_py_thread_id();
        self.start_sync_prediction(slot, py_thread_id);

        // Check cancellation first (in case cancel was called before we started)
        if self.take_cancelled(slot) {
            self.finish_prediction(slot);
            return PredictResult::cancelled(0.0);
        }

        // Enter prediction context - sets cog_prediction_id ContextVar for log routing
        let prediction_id = id.clone();
        let slot_sender_clone = slot_sender.clone();
        let log_guard = Python::attach(|py| {
            crate::log_writer::PredictionLogGuard::enter(
                py,
                prediction_id.clone(),
                slot_sender_clone,
            )
        });
        let log_guard = match log_guard {
            Ok(g) => Some(g),
            Err(e) => {
                tracing::warn!(error = %e, "Failed to enter prediction context");
                None
            }
        };

        // Enter metric scope - sets Scope ContextVar for metric recording
        let slot_sender_for_metrics = slot_sender.clone();
        let scope_guard = Python::attach(|py| {
            crate::metric_scope::ScopeGuard::enter(py, slot_sender_for_metrics)
        });
        let scope_guard = match scope_guard {
            Ok(g) => Some(g),
            Err(e) => {
                tracing::warn!(error = %e, "Failed to enter metric scope");
                None
            }
        };

        tracing::trace!(%slot, %id, "Prediction context entered");

        // Run prediction or training based on mode.
        let start = std::time::Instant::now();

        let result = match self.mode {
            HandlerMode::Train => {
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
                    // Async train - submit to shared event loop
                    let loop_obj = match self.get_async_loop() {
                        Some(l) => l,
                        None => {
                            return PredictResult::failed(
                                "Async event loop not initialized".to_string(),
                                start.elapsed().as_secs_f64(),
                            );
                        }
                    };

                    // Submit coroutine and get future + prepared input for cleanup
                    let (future, is_async_gen, prepared) = match pred
                        .train_async_worker(input, &loop_obj, &id)
                    {
                        Ok(f) => f,
                        Err(e) => {
                            self.finish_prediction(slot);
                            drop(log_guard);
                            return if matches!(e, coglet_core::PredictionError::Cancelled) {
                                PredictResult::cancelled(start.elapsed().as_secs_f64())
                            } else {
                                PredictResult::failed(e.to_string(), start.elapsed().as_secs_f64())
                            };
                        }
                    };

                    // Update slot state with future for cancellation
                    Python::attach(|py| {
                        self.start_async_prediction(slot, future.clone_ref(py));
                    });

                    // Block on future.result()
                    let sender_for_async = slot_sender.clone();
                    let result = Python::attach(|py| match future.call_method0(py, "result") {
                        Ok(result) => pred.process_async_result(
                            py,
                            result.bind(py),
                            is_async_gen,
                            &sender_for_async,
                        ),
                        Err(e) => {
                            let err_str = e.to_string();
                            if err_str.contains("CancelledError") || err_str.contains("cancelled") {
                                Err(coglet_core::PredictionError::Cancelled)
                            } else {
                                Err(coglet_core::PredictionError::Failed(format!(
                                    "Async training failed: {}",
                                    e
                                )))
                            }
                        }
                    });

                    // Cleanup temp files via RAII
                    drop(prepared);

                    result
                } else {
                    // Sync train - set sync prediction ID for log routing
                    crate::log_writer::set_sync_prediction_id(Some(&id));
                    let _cancelable = crate::cancel::enter_cancelable();
                    let r = pred.train_worker(input, slot_sender.clone());
                    crate::log_writer::set_sync_prediction_id(None);

                    // Upgrade to Cancelled if the slot was marked cancelled
                    // (same logic as sync predict above)
                    match r {
                        Err(_) if self.is_cancelled(slot) => {
                            Err(coglet_core::PredictionError::Cancelled)
                        }
                        other => other,
                    }
                }
            }
            HandlerMode::Predict => {
                // Prediction mode
                tracing::trace!(%slot, %id, is_async = pred.is_async(), "Running prediction");
                if pred.is_async() {
                    // Async predict - submit to shared event loop
                    let loop_obj = match self.get_async_loop() {
                        Some(l) => l,
                        None => {
                            return PredictResult::failed(
                                "Async event loop not initialized".to_string(),
                                start.elapsed().as_secs_f64(),
                            );
                        }
                    };

                    // Submit coroutine and get future + prepared input for cleanup
                    let (future, is_async_gen, prepared) = match pred
                        .predict_async_worker(input, &loop_obj, &id)
                    {
                        Ok(f) => f,
                        Err(e) => {
                            self.finish_prediction(slot);
                            drop(log_guard);
                            return if matches!(e, coglet_core::PredictionError::Cancelled) {
                                PredictResult::cancelled(start.elapsed().as_secs_f64())
                            } else {
                                PredictResult::failed(e.to_string(), start.elapsed().as_secs_f64())
                            };
                        }
                    };

                    // Update slot state with future for cancellation
                    Python::attach(|py| {
                        self.start_async_prediction(slot, future.clone_ref(py));
                    });

                    // Block on future.result()
                    let sender_for_async = slot_sender.clone();
                    let result = Python::attach(|py| match future.call_method0(py, "result") {
                        Ok(result) => pred.process_async_result(
                            py,
                            result.bind(py),
                            is_async_gen,
                            &sender_for_async,
                        ),
                        Err(e) => {
                            let err_str = e.to_string();
                            if err_str.contains("CancelledError") || err_str.contains("cancelled") {
                                Err(coglet_core::PredictionError::Cancelled)
                            } else {
                                Err(coglet_core::PredictionError::Failed(format!(
                                    "Async prediction failed: {}",
                                    e
                                )))
                            }
                        }
                    });

                    // Cleanup temp files via RAII
                    drop(prepared);

                    result
                } else {
                    // Sync predict - set sync prediction ID for log routing
                    crate::log_writer::set_sync_prediction_id(Some(&id));
                    let _cancelable = crate::cancel::enter_cancelable();
                    tracing::trace!(%slot, %id, "Calling predict_worker");
                    let r = pred.predict_worker(input, slot_sender.clone());
                    tracing::trace!(%slot, %id, "predict_worker returned");
                    crate::log_writer::set_sync_prediction_id(None);

                    // If the prediction failed AND the slot was marked cancelled,
                    // treat it as a cancellation. PyThreadState_SetAsyncExc injects
                    // CancelationException which predict_worker sees as a generic
                    // PyErr — we upgrade it to Cancelled here.
                    match r {
                        Err(_) if self.is_cancelled(slot) => {
                            Err(coglet_core::PredictionError::Cancelled)
                        }
                        other => other,
                    }
                }
            }
        };
        tracing::trace!(%slot, %id, "Prediction completed");

        self.finish_prediction(slot);

        // Exit prediction context
        drop(scope_guard);
        drop(log_guard);

        match result {
            Ok(r) => {
                PredictResult::success(output_to_json(r.output), start.elapsed().as_secs_f64())
            }
            Err(e) => {
                if matches!(e, coglet_core::PredictionError::Cancelled) {
                    PredictResult::cancelled(start.elapsed().as_secs_f64())
                } else {
                    PredictResult::failed(e.to_string(), start.elapsed().as_secs_f64())
                }
            }
        }
    }

    fn cancel(&self, slot: SlotId) {
        // Mark slot as cancelled and determine how to cancel based on state
        let mut slots = self.slots.lock().expect("slots mutex poisoned");

        if let Some(state) = slots.get_mut(&slot) {
            state.mark_cancelled();

            match state {
                SlotState::AsyncPrediction { .. } => {
                    drop(slots); // Release lock before calling cancel_async_future
                    // Async: cancel via future.cancel()
                    if !self.cancel_async_future(slot) {
                        tracing::trace!(%slot, "No async future to cancel (prediction may have completed)");
                    }
                }
                SlotState::SyncPrediction { py_thread_id, .. } => {
                    let py_thread_id = *py_thread_id;
                    drop(slots); // Release lock
                    // Sync: inject CancelationException into the Python thread
                    // via PyThreadState_SetAsyncExc (fires at next bytecode boundary)
                    crate::cancel::cancel_sync_thread(py_thread_id);
                }
                SlotState::Idle => {
                    // Already idle, nothing to cancel
                    tracing::trace!(%slot, "Cancel called on idle slot");
                }
            }
        } else {
            tracing::trace!(%slot, "Cancel called on unknown slot");
        }
    }

    fn schema(&self) -> Option<serde_json::Value> {
        let guard = self.predictor.lock().expect("predictor mutex poisoned");
        guard.as_ref().and_then(|pred| pred.schema(self.mode))
    }

    async fn healthcheck(&self) -> coglet_core::orchestrator::HealthcheckResult {
        // Get predictor
        let pred = {
            let guard = self.predictor.lock().expect("predictor mutex poisoned");
            match guard.as_ref() {
                Some(p) => Arc::clone(p),
                None => {
                    return coglet_core::orchestrator::HealthcheckResult::unhealthy(
                        "Predictor not initialized",
                    );
                }
            }
        };

        // Check if predictor has a healthcheck method
        let has_healthcheck = Python::attach(|py| pred.has_healthcheck(py));
        if !has_healthcheck {
            // No healthcheck defined = healthy
            return coglet_core::orchestrator::HealthcheckResult::healthy();
        }

        // Run healthcheck with timeout
        let is_async = Python::attach(|py| pred.is_healthcheck_async(py));

        if is_async {
            // Async healthcheck - run in event loop with timeout
            let loop_obj = match self.get_async_loop() {
                Some(l) => l,
                None => {
                    return coglet_core::orchestrator::HealthcheckResult::unhealthy(
                        "Async event loop not initialized",
                    );
                }
            };

            Python::attach(|py| pred.healthcheck_async(py, &loop_obj))
        } else {
            // Sync healthcheck - run in thread pool with timeout
            Python::attach(|py| pred.healthcheck_sync(py))
        }
    }
}

/// Shutdown the asyncio event loop and join the thread.
impl Drop for PythonPredictHandler {
    fn drop(&mut self) {
        // Stop the event loop
        if let Some(loop_obj) = self
            .async_loop
            .lock()
            .expect("async_loop mutex poisoned")
            .take()
        {
            Python::attach(|py| {
                let loop_ref = loop_obj.bind(py);
                // Get the stop method and schedule it via call_soon_threadsafe
                match loop_ref.getattr("stop") {
                    Ok(stop_method) => {
                        if let Err(e) =
                            loop_ref.call_method1("call_soon_threadsafe", (stop_method,))
                        {
                            tracing::warn!(error = %e, "Failed to stop asyncio loop");
                        }
                    }
                    Err(e) => {
                        tracing::warn!(error = %e, "Failed to get loop.stop method");
                    }
                }
            });
        }

        // Join the thread
        if let Some(thread) = self
            .async_thread
            .lock()
            .expect("async_thread mutex poisoned")
            .take()
            && let Err(e) = thread.join()
        {
            tracing::warn!("Failed to join asyncio loop thread: {:?}", e);
        }
    }
}

/// Convert PredictionOutput to serde_json::Value
fn output_to_json(output: coglet_core::PredictionOutput) -> serde_json::Value {
    match output {
        coglet_core::PredictionOutput::Single(v) => v,
        coglet_core::PredictionOutput::Stream(v) => serde_json::Value::Array(v),
    }
}
