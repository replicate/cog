//! Worker-side code - runs in the subprocess.
//!
//! Architecture:
//! - Control channel (stdin/stdout): Cancel, Shutdown signals + Ready, Idle responses
//! - Slot sockets: Prediction data + streaming logs
//!
//! Each slot runs predictions independently. Idle sent on control channel when
//! prediction completes.

use std::io;
use std::sync::Arc;
use std::time::Instant;

use futures::{SinkExt, StreamExt};
use tokio::io::{stdin, stdout};
use tokio::sync::mpsc;
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::codec::JsonCodec;
use crate::protocol::{ControlRequest, ControlResponse, LogSource, SlotRequest, SlotResponse};
use crate::transport::{connect_transport, get_transport_info_from_env, SlotTransport};

/// Trait for the prediction handler - abstracts the Python integration.
#[async_trait::async_trait]
pub trait PredictHandler: Send + Sync + 'static {
    /// Initialize the predictor (load model, run setup).
    async fn setup(&self) -> Result<(), String>;

    /// Run a prediction. Called with slot index and prediction ID.
    async fn predict(&self, slot: usize, id: String, input: serde_json::Value) -> PredictResult;

    /// Request cancellation of prediction on a slot.
    fn cancel(&self, slot: usize);

    /// Get OpenAPI schema for the predictor.
    fn schema(&self) -> Option<serde_json::Value> {
        None
    }
}

/// Result of a prediction.
#[derive(Debug)]
pub struct PredictResult {
    pub output: serde_json::Value,
    pub success: bool,
    pub error: Option<String>,
    pub predict_time: f64,
}

impl PredictResult {
    pub fn success(output: serde_json::Value, predict_time: f64) -> Self {
        Self {
            output,
            success: true,
            error: None,
            predict_time,
        }
    }

    pub fn failed(error: String, predict_time: f64) -> Self {
        Self {
            output: serde_json::Value::Null,
            success: false,
            error: Some(error),
            predict_time,
        }
    }

    pub fn cancelled(predict_time: f64) -> Self {
        Self {
            output: serde_json::Value::Null,
            success: false,
            error: Some("Cancelled".to_string()),
            predict_time,
        }
    }
}

/// Worker configuration.
pub struct WorkerConfig {
    /// Number of concurrent prediction slots.
    pub num_slots: usize,
}

impl Default for WorkerConfig {
    fn default() -> Self {
        Self { num_slots: 1 }
    }
}

/// Completion message from a slot task.
struct SlotCompletion {
    slot: usize,
    id: String,
    result: PredictResult,
    poisoned: bool,
}

/// Run the worker event loop.
///
/// Reads control messages from stdin, prediction requests from slot sockets.
/// Spawns async tasks for concurrent predictions.
pub async fn run_worker<H: PredictHandler>(handler: Arc<H>, config: WorkerConfig) -> io::Result<()> {
    let num_slots = config.num_slots;

    // Connect to slot sockets (transport info from env, set by parent)
    let child_info = get_transport_info_from_env()?;
    let mut transport = connect_transport(child_info).await?;

    // Control channel
    let mut ctrl_reader = FramedRead::new(stdin(), JsonCodec::<ControlRequest>::new());
    let mut ctrl_writer = FramedWrite::new(stdout(), JsonCodec::<ControlResponse>::new());

    // Run setup
    tracing::info!("Worker starting setup");
    if let Err(e) = handler.setup().await {
        tracing::error!(error = %e, "Setup failed");
        let _ = ctrl_writer
            .send(ControlResponse::Failed {
                slot: 0,
                error: format!("Setup failed: {}", e),
            })
            .await;
        return Ok(());
    }

    // Send Ready with schema
    let schema = handler.schema();
    tracing::info!(num_slots, "Worker ready");
    ctrl_writer.send(ControlResponse::Ready { schema }).await?;

    // Channel for slot completions
    let (completion_tx, mut completion_rx) = mpsc::channel::<SlotCompletion>(num_slots);

    // Track slot state
    let mut slot_busy = vec![false; num_slots];
    let mut slot_poisoned = vec![false; num_slots];

    loop {
        tokio::select! {
            // Control channel messages
            ctrl_msg = ctrl_reader.next() => {
                match ctrl_msg {
                    Some(Ok(ControlRequest::Cancel { slot })) => {
                        tracing::debug!(slot, "Cancel requested");
                        handler.cancel(slot);
                    }
                    Some(Ok(ControlRequest::Shutdown)) => {
                        tracing::info!("Shutdown requested");
                        let _ = ctrl_writer.send(ControlResponse::ShuttingDown).await;
                        break;
                    }
                    Some(Err(e)) => {
                        tracing::error!(error = %e, "Control channel error");
                        break;
                    }
                    None => {
                        // Parent closed control channel - exit
                        tracing::warn!("Control channel closed (parent died?), exiting");
                        break;
                    }
                }
            }

            // Slot completions
            Some(completion) = completion_rx.recv() => {
                let slot = completion.slot;
                slot_busy[slot] = false;

                if completion.poisoned {
                    slot_poisoned[slot] = true;
                    let _ = ctrl_writer.send(ControlResponse::Failed {
                        slot,
                        error: completion.result.error.unwrap_or_default(),
                    }).await;
                } else {
                    let _ = ctrl_writer.send(ControlResponse::Idle { slot }).await;
                }

                // Check if all slots poisoned
                if slot_poisoned.iter().all(|&p| p) {
                    tracing::error!("All slots poisoned, exiting");
                    break;
                }
            }
        }

        // Check for new prediction requests on idle slots
        for slot in 0..num_slots {
            if slot_busy[slot] || slot_poisoned[slot] {
                continue;
            }

            // Try to read from this slot (non-blocking check)
            let socket = match transport.slot_socket(slot) {
                Some(s) => s,
                None => continue,
            };

            // TODO: properly poll slot sockets for new requests
            // For now, this is a placeholder - need async select over slot sockets
        }
    }

    tracing::info!("Worker exiting");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn predict_result_success() {
        let r = PredictResult::success(serde_json::json!("hello"), 0.5);
        assert!(r.success);
        assert!(r.error.is_none());
    }

    #[test]
    fn predict_result_failed() {
        let r = PredictResult::failed("oops".into(), 0.5);
        assert!(!r.success);
        assert_eq!(r.error, Some("oops".to_string()));
    }

    #[test]
    fn predict_result_cancelled() {
        let r = PredictResult::cancelled(0.5);
        assert!(!r.success);
    }

    #[test]
    fn worker_config_default() {
        let config = WorkerConfig::default();
        assert_eq!(config.num_slots, 1);
    }
}
