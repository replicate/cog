//! Bridge between coglet worker and Python predictor.

use std::sync::{Arc, Mutex};

use pyo3::prelude::*;

use coglet_core::bridge::protocol::SlotId;
use coglet_core::worker::{PredictHandler, PredictResult, SlotSender};

use crate::predictor::PythonPredictor;

/// PredictHandler implementation that wraps a Python predictor.
pub struct PythonPredictHandler {
    predictor_ref: String,
    is_train: bool,
    /// Predictor object - only accessed from blocking tasks with GIL
    predictor: Mutex<Option<PythonPredictor>>,
}

impl PythonPredictHandler {
    pub fn new(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            is_train: false,
            predictor: Mutex::new(None),
        }
    }

    pub fn new_train(predictor_ref: String) -> Self {
        Self {
            predictor_ref,
            is_train: true,
            predictor: Mutex::new(None),
        }
    }
}

#[async_trait::async_trait]
impl PredictHandler for PythonPredictHandler {
    async fn setup(&self) -> Result<(), String> {
        let predictor_ref = self.predictor_ref.clone();
        let is_train = self.is_train;

        let loaded_predictor = tokio::task::spawn_blocking(move || {
            let mut predictor = if is_train {
                PythonPredictor::new_train(predictor_ref)
            } else {
                PythonPredictor::new(predictor_ref)
            };
            predictor.load()?;
            predictor.setup()?;
            Ok::<PythonPredictor, String>(predictor)
        })
        .await
        .map_err(|e| format!("Task panicked: {}", e))??;

        *self.predictor.lock().unwrap() = Some(loaded_predictor);
        Ok(())
    }

    async fn predict(
        &self,
        slot: SlotId,
        id: String,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> PredictResult {
        // Get a clone of the predictor info needed for prediction
        // PyObject must be cloned inside GIL context
        let (predictor_ref, is_train, predictor_obj, schema) = {
            let guard = self.predictor.lock().unwrap();
            let p = guard.as_ref();
            let obj = p.and_then(|p| {
                // Clone PyObject inside GIL
                Python::with_gil(|py| {
                    p.predictor.as_ref().map(|obj| obj.clone_ref(py))
                })
            });
            (
                self.predictor_ref.clone(),
                self.is_train,
                obj,
                p.and_then(|p| p.schema.clone()),
            )
        };

        tokio::task::spawn_blocking(move || {
            let predictor = PythonPredictor {
                predictor_ref,
                is_train,
                predictor: predictor_obj,
                schema,
            };
            predictor.predict(slot, id, input, slot_sender)
        })
        .await
        .unwrap_or_else(|e| PredictResult::failed(format!("Task panicked: {}", e), 0.0))
    }

    fn cancel(&self, _slot: SlotId) {
        // Send SIGUSR1 to interrupt blocking Python code
        #[cfg(unix)]
        {
            use std::process;
            unsafe {
                libc::kill(process::id() as i32, libc::SIGUSR1);
            }
        }
    }

    fn schema(&self) -> Option<serde_json::Value> {
        self.predictor.lock().unwrap().as_ref().and_then(|p| p.schema())
    }
}
